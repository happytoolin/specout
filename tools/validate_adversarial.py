#!/usr/bin/env python3
"""Run launch regression tests, then validate actual Go-serialized payloads.

Payload checks still run when Go assertions fail, so one defect cannot hide
the other launch blockers. Uses the same dependencies as validate_suite.py.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile

from jsonschema import Draft202012Validator
from openapi_spec_validator import validate

ROOT = Path(__file__).resolve().parents[1]


def main():
    with tempfile.TemporaryDirectory(prefix="specout-adversarial-") as directory:
        result = subprocess.run(
            ["go", "test", "-json", "-count=1", "-run", "^(TestLaunch|FuzzLaunch)", "./..."],
            cwd=ROOT, capture_output=True, text=True, check=False,
            env={**os.environ, "SPECOUT_VALIDATE_DIR": directory},
        )
        failures = []
        for line in result.stdout.splitlines():
            event = json.loads(line)
            if event.get("Action") == "fail" and event.get("Test"):
                failures.append(f"{event['Package']}: {event['Test']}")
        for failure in failures:
            print(f"FAIL Go: {failure}")
        if result.returncode and not failures:
            print(result.stdout)
        if result.stderr:
            print(result.stderr)

        files = sorted(Path(directory).glob("*.json"))
        if not files:
            raise AssertionError("no launch test documents were exported")
        payload_count = 0
        schema_failures = 0
        for path in files:
            document = json.loads(path.read_text())
            try:
                validate(document)
                for schema in document.get("components", {}).get("schemas", {}).values():
                    Draft202012Validator.check_schema(schema)
                for case in document.get("x-specout-test-cases", []):
                    payload_count += 1
                    validator = Draft202012Validator({
                        "allOf": [case["schema"]], "components": document.get("components", {}),
                    })
                    errors = list(validator.iter_errors(case["value"]))
                    if (not errors) != case["valid"]:
                        detail = errors[0].message if errors else "accepted a forbidden payload"
                        print(f"FAIL payload: {path.name}: {case['name']}: {detail}")
                        schema_failures += 1
            except Exception as error:
                print(f"FAIL schema: {path.name}: {error}")
                schema_failures += 1
        if payload_count == 0:
            raise AssertionError("no serialized payload cases were exported")
        print(f"Checked {len(files)} documents and {payload_count} serialized payload cases; "
              f"Go exit={result.returncode}, schema/payload failures={schema_failures}")
        return int(result.returncode != 0 or schema_failures != 0)


if __name__ == "__main__":
    raise SystemExit(main())
