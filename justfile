# specout dev tasks. just --list to see everything.

# toolchain-owned gofmt (PATH gofmt may be older)
gofmt := `go env GOROOT` + "/bin/gofmt"

# default: what CI runs
default: lint test check-golden

# validate the golden spec against the official OpenAPI 3.1 schema
# (creates a local venv on first run; openapi-spec-validator is the
# reference validator from the FastAPI ecosystem)
validate:
    #!/bin/sh
    if [ ! -x .venv/bin/python ]; then python3 -m venv .venv && .venv/bin/pip install -q openapi-spec-validator; fi
    .venv/bin/python tools/validate_spec.py testdata/openapi.json

# build every package
build:
    go build ./...

# run the full test suite
test:
    go test ./...

# vet + gofmt (add staticcheck once a go1.27 build is available)
lint:
    go vet ./...
    @! {{gofmt}} -l . | grep -q .

# serve the demo: swagger ui at /, scalar at /scalar
demo:
    go run ./cmd/demo

# export the spec to stdout (CI golden uses this same path)
spec:
    GO_SPEC_ONLY=1 go run ./cmd/demo

# regenerate testdata/openapi.json after intentional spec changes
golden:
    UPDATE_GOLDEN=1 go test -run TestGoldenSpec .

# fail if the committed golden differs from what the binary emits
check-golden:
    GO_SPEC_ONLY=1 go run ./cmd/demo > /tmp/specout-spec.json
    cmp /tmp/specout-spec.json testdata/openapi.json || (echo "stale: run just golden" && exit 1)
    @echo "golden ok"

# format, lint, test — the pre-commit sweep
sweep: fmt lint test

fmt:
    gofmt -w .
