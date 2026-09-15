#!/usr/bin/env python3
"""Validate an OpenAPI document against the official 3.1 JSON Schema.

Usage: ./tools/validate_spec.py [path-or-url] (default: testdata/openapi.json)
Exits non-zero with a readable error list on any violation.
"""
import json
import sys

from openapi_spec_validator import validate


def main() -> int:
    src = sys.argv[1] if len(sys.argv) > 1 else "testdata/openapi.json"
    with open(src) as f:
        doc = json.load(f)
    try:
        validate(doc)
    except Exception as e:
        errs = getattr(e, "messages", None) or [str(e)]
        for m in errs:
            print(m, file=sys.stderr)
        return 1
    version = doc.get("openapi", "?")
    n_paths = len(doc.get("paths", {}))
    n_schemas = len(doc.get("components", {}).get("schemas", {}))
    print(f"OK: {src} — OpenAPI {version}, {n_paths} paths, {n_schemas} schemas")
    return 0


if __name__ == "__main__":
    sys.exit(main())
