# specout dev tasks. just --list to see everything.

# One pinned tool. golangci-lint v2 bundles gofumpt (as a formatter) and go
# vet, so lint and format cannot drift apart. `go run` needs no install step.
lintbin := "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2"

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

# report every lint and format finding; changes nothing
lint:
    {{ lintbin }} run ./...

# repair: sync go.mod, format, and apply every auto-fix
tidy:
    go mod tidy
    {{ lintbin }} fmt ./...
    {{ lintbin }} run --fix ./...
    gofumpt -l -w .   

# format only (gofumpt, through the same pinned linter)
fmt:
    {{ lintbin }} fmt ./...

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

# tidy, lint, test — the pre-commit sweep
sweep: tidy lint test
