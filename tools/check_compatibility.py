#!/usr/bin/env python3
"""Compare typed API samples with pinned upstream contracts.

Run: .venv/bin/python tools/check_compatibility.py
Refresh source excerpts: add --refresh (requires network access).
Only the declared operation subset is tested; this is not an OpenAPI importer.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.request

from openapi_spec_validator import validate

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "examples/compatibility/testdata"
# Ignore only annotations. Unknown constraint keywords must remain visible in
# the diff; an allow-list could silently hide a new source requirement.
ANNOTATIONS = {"title", "description", "example", "examples", "externalDocs", "xml", "deprecated"}


def dereference(node, document, stack=()):
    if isinstance(node, list):
        return [dereference(value, document, stack) for value in node]
    if not isinstance(node, dict):
        return node
    if "$ref" in node:
        ref = node["$ref"]
        if ref in stack or not ref.startswith("#/"):
            raise ValueError(f"unexpected cyclic or external ref in sample: {ref}")
        target = document
        for key in ref[2:].split("/"):
            target = target[key.replace("~1", "/").replace("~0", "~")]
        return dereference({**target, **{k:v for k,v in node.items() if k != "$ref"}}, document, (*stack, ref))
    return {key: dereference(value, document, stack) for key, value in node.items()}


def canonical_schema(schema):
    if isinstance(schema, bool):
        return schema
    result = {}
    for key, value in schema.items():
        if key in ANNOTATIONS or key.startswith("x-"):
            continue
        if key in {"properties", "patternProperties", "dependentSchemas", "$defs"}:
            value = {name: canonical_schema(prop) for name, prop in value.items()}
        elif key in {"items", "additionalProperties", "not", "contains", "propertyNames", "unevaluatedProperties", "unevaluatedItems", "if", "then", "else"}:
            value = canonical_schema(value)
        elif key in {"oneOf", "anyOf", "allOf", "prefixItems"}:
            value = [canonical_schema(arm) for arm in value]
        elif key in {"required", "enum"} or key == "type" and isinstance(value, list):
            value = sorted(value, key=lambda item: json.dumps(item, sort_keys=True))
        result[key] = value
    if result.pop("nullable", False) and "type" in result:
        types = result["type"] if isinstance(result["type"], list) else [result["type"]]
        result["type"] = sorted(set([*types, "null"]))
    # These source allOfs only compose object properties. Merge their actual
    # constraints, rather than treating inheritance spelling as a mismatch.
    if "allOf" in result:
        arms = result.pop("allOf")
        for arm in arms:
            for key, value in arm.items():
                if key == "properties":
                    current = result.setdefault(key, {})
                    for prop, shape in value.items():
                        if prop in current and current[prop] != shape:
                            raise ValueError(f"conflicting allOf property: {prop}")
                        current[prop] = shape
                elif key == "required":
                    result[key] = sorted(set(result.get(key, [])) | set(value))
                elif key in result and result[key] != value:
                    raise ValueError(f"unsupported allOf constraint merge: {key}")
                else:
                    result[key] = value
    # The samples compare object payloads. Some Cloudflare object schemas omit
    # type but have properties; preserve that documented normalization explicitly.
    if "properties" in result:
        result.setdefault("type", "object")
    if result.get("additionalProperties") is True:
        result.pop("additionalProperties")
    return result


def parameter_contract(parameter):
    location = parameter["in"]
    style = parameter.get("style", "simple" if location in {"path", "header"} else "form")
    return {
        "name": parameter["name"], "in": location,
        "required": parameter.get("required", False), "style": style,
        "explode": parameter.get("explode", style == "form"),
        "schema": canonical_schema(parameter["schema"]),
    }


def contract(operation, success_only):
    result = {
        "operationId": operation["operationId"],
        "parameters": sorted([parameter_contract(p) for p in operation.get("parameters", [])], key=lambda p: (p["in"], p["name"])),
        "responses": {},
    }
    for code, response in operation["responses"].items():
        result["responses"][code] = {} if success_only and not code.startswith("2") else {
            media: canonical_schema(body["schema"])
            for media, body in response.get("content", {}).items()
        }
    if "requestBody" in operation:
        body = operation["requestBody"]
        result["requestBody"] = {
            "required": body.get("required", False),
            "content": {media: canonical_schema(item["schema"]) for media, item in body["content"].items()},
        }
    return result


def refresh(source):
    with urllib.request.urlopen(source["url"], timeout=60) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != source["sha256"]:
        raise ValueError(f"{source['name']}: source SHA-256 changed")
    upstream = json.loads(data)
    contracts = {}
    for selection in source["operations"]:
        method, path = selection.split(" ", 1)
        operation = upstream["paths"][path][method.lower()]
        # Exclude broad error payloads before resolving their enormous graph.
        if source["success_only"]:
            operation = {**operation, "responses": {code: response if code.startswith("2") else {} for code, response in operation["responses"].items()}}
        contracts[selection] = contract(dereference(operation, upstream), source["success_only"])
    target = FIXTURES / (source["name"] + ".json")
    target.write_text(json.dumps(contracts, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--refresh", action="store_true")
    args = parser.parse_args()
    sources = json.loads((FIXTURES / "sources.json").read_text())
    count = 0
    for source in sources:
        if args.refresh:
            refresh(source)
        expected = json.loads((FIXTURES / (source["name"] + ".json")).read_text())
        output = subprocess.check_output(["go", "run", "./examples/compatibility", source["name"]], cwd=ROOT)
        generated = json.loads(output)
        validate(generated)
        for selection, want in expected.items():
            method, path = selection.split(" ", 1)
            operation = dereference(generated["paths"][path][method.lower()], generated)
            got = contract(operation, source["success_only"])
            if got != want:
                import difflib
                diff = "\n".join(difflib.unified_diff(json.dumps(want, indent=2, sort_keys=True).splitlines(), json.dumps(got, indent=2, sort_keys=True).splitlines(), fromfile="upstream", tofile="generated"))
                raise AssertionError(f"{source['name']} {selection}\n{diff}")
            count += 1
        print(f"PASS {source['name']}: valid OpenAPI 3.1; {len(expected)} upstream operation contracts match")
    print(f"PASS {count} operations across {len(sources)} pinned public specifications")


if __name__ == "__main__":
    main()
