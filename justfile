# specout dev tasks. just --list to see everything.

# One pinned tool. golangci-lint v2 bundles gofumpt (as a formatter) and go
# vet, so lint and format cannot drift apart. `go run` needs no install step.
lintbin := "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2"

# default: quick local checks; CI also runs build, race, validate and client-types
default: lint test check-golden

# install the pinned validation tools in an isolated environment
validation-deps:
    #!/bin/sh
    if [ ! -x .venv/bin/python ]; then python3 -m venv .venv; fi
    .venv/bin/pip install -q -r tools/requirements.txt

# validate the golden, test documents, examples and selected upstream contracts
validate: validation-deps
    .venv/bin/python tools/test_contract_comparison.py
    .venv/bin/python tools/validate_spec.py testdata/openapi.json
    .venv/bin/python tools/validate_suite.py
    .venv/bin/python tools/check_compatibility.py

# refresh pinned upstream excerpts and verify their checksums (network required)
compatibility-refresh: validation-deps
    .venv/bin/python tools/check_compatibility.py --refresh

# check all packages with the race detector
race:
    go test -race ./...

# generate and compile client types for all six examples (Node.js/npm required)
client-types:
    ./tools/check_client_types.sh

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
