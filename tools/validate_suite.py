#!/usr/bin/env python3
"""Validate test output and all served examples, including union payloads."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

from jsonschema import Draft202012Validator
from openapi_spec_validator import validate

ROOT = Path(__file__).resolve().parents[1]


def main():
    with tempfile.TemporaryDirectory(prefix="specout-validation-") as directory:
        subprocess.run(
            ["go", "test", "-count=1", "."], cwd=ROOT, check=True,
            env={**os.environ, "SPECOUT_VALIDATE_DIR": directory},
        )
        files = sorted(Path(directory).glob("*.json"))
        if not files:
            raise AssertionError("no test documents were exported")
        documents = [(path.name, json.loads(path.read_text())) for path in files]
        for package in ("./cmd/demo", "./examples/petstore", "./examples/msgraph"):
            output = subprocess.check_output(
                ["go", "run", package], cwd=ROOT,
                env={**os.environ, "GO_SPEC_ONLY": "1"},
            )
            documents.append((package, json.loads(output)))
        for name, document in documents:
            try:
                validate(document)
                for schema in document.get("components", {}).get("schemas", {}).values():
                    Draft202012Validator.check_schema(schema)
            except Exception as error:
                raise AssertionError(f"{name}: {error}") from error
        print(f"PASS {len(files)} test documents and 3 served examples: OpenAPI 3.1 and JSON Schema")

        demo = next(document for name, document in documents if name == "./cmd/demo")
        validator = Draft202012Validator({
            "$ref": "#/components/schemas/Config", "components": demo["components"],
        })
        validator.validate({"kind": "email", "data": {"address": "ops@example.com"}})
        validator.validate({"kind": "slack", "data": {"channel": "#ops"}})
        for payload in (
            {"kind": "email", "data": {"channel": "#ops"}},
            {"kind": "slack", "data": {"address": "ops@example.com"}},
            {"kind": "unknown", "data": {}},
            {"data": {"address": "ops@example.com"}},
        ):
            if validator.is_valid(payload):
                raise AssertionError(f"union accepted invalid payload: {payload}")
        print("PASS union payloads: matching variants accepted; mismatches rejected")

        for name, document in documents:
            if "TestUnionBranches" not in name:
                continue
            validator = Draft202012Validator({
                "$ref": "#/components/schemas/unionEnvelope",
                "components": document["components"],
            })
            payload = {
                "kind": "email", "data": {"address": "ops@example.com"},
                "trace_id": None, "meta": {"source": "test", "note": None},
            }
            validator.validate(payload)
            if validator.is_valid({k: v for k, v in payload.items() if k != "meta"}):
                raise AssertionError(f"{name}: required envelope field was lost")
            if "CloseObjects" in name and validator.is_valid({**payload, "extra": True}):
                raise AssertionError(f"{name}: closed envelope accepted an extra field")
        print("PASS union envelope fields, nullable values and closed objects")

        for name, document in documents:
            component = None
            if name.startswith("TestClosedNamedUnionRoot"):
                component = "unionNamedHolder"
                payload = {"envelope": {
                    "kind": "email", "data": {"address": "ops@example.com"},
                    "meta": {"source": "test", "note": None}, "trace_id": None,
                }}
            elif name.startswith(("TestClosedNestedUnionRoot", "TestNestedUnionsUseDistinct")):
                component = "unionNestedHolder"
                payload = {
                    "email": {"kind": "email", "data": {"address": "ops@example.com"}},
                    "slack": {"kind": "slack", "data": {"channel": "#ops"}},
                }
            if component:
                validator = Draft202012Validator({
                    "$ref": "#/components/schemas/" + component,
                    "components": document["components"],
                })
                validator.validate(payload)
                if "envelope" in payload:
                    payload["envelope"]["extra"] = True
                else:
                    payload["email"]["data"] = {"channel": "#ops"}
                if validator.is_valid(payload):
                    raise AssertionError(f"{name}: nested union accepted invalid payload")
        print("PASS nested union payloads")

        enum_doc = next(document for name, document in documents
                        if name.startswith("TestParameterEnumsSplitBeforeNullability"))
        for parameter in enum_doc["paths"]["/items/{id}"]["get"]["parameters"]:
            validator = Draft202012Validator(parameter["schema"])
            validator.validate("a")
            validator.validate("b")
            if validator.is_valid("a|b") or validator.is_valid("c"):
                raise AssertionError(f"{parameter['name']}: parameter enum accepted an invalid value")
            if validator.is_valid(None) != (parameter["in"] != "path"):
                raise AssertionError(f"{parameter['name']}: incorrect parameter nullability")
        print("PASS parameter enums: valid values accepted; unsplit and unknown values rejected")


if __name__ == "__main__":
    main()
