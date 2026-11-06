# specout dev tasks. just --list to see everything.

# golangci-lint v2 bundles gofumpt (as a formatter) and go
# vet, so lint and format cannot drift apart. `go run` needs no install step.
lintbin := "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"

# the root module, each adapter module, and the examples module: each has its
# own go.mod
modules := ". adapters/specoutchi adapters/specoutgorilla adapters/specoutecho adapters/specoutechov4 adapters/specoutgin adapters/specoutfiber examples"

# default: quick local checks; CI and releases run check
default: lint test

# run every CI and release check (Go, Python 3 and Node.js/npm required)
check: mod-check build lint nilaway workflow-lint test race validate client-types vuln

# check module files and downloaded dependency integrity without changing files
mod-check:
    for m in {{ modules }}; do (cd $m && go mod tidy -diff && go mod verify) || exit 1; done

# validate GitHub Actions workflows
workflow-lint:
    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

# report nil flows that can panic at run time. Library and example code only:
# nilaway cannot see that a testify require stops the test, so on test files
# it reports only false positives.
nilaway:
    for m in {{ modules }}; do (cd $m && go run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260918162853-acb8859b9031 -include-pkgs=github.com/happytoolin/specout -test=false ./...) || exit 1; done

# check code and tests against the current Go vulnerability database
vuln:
    for m in {{ modules }}; do (cd $m && go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -test ./...) || exit 1; done

# install the pinned validation tools in an isolated environment
validation-deps:
    #!/bin/sh
    if [ ! -x .venv/bin/python ]; then python3 -m venv .venv; fi
    .venv/bin/pip install -q -r tools/requirements.txt

# validate generated test documents, served examples and payload cases
validate: validation-deps
    .venv/bin/python tools/validate_suite.py

# check all packages with the race detector, in random test order
race:
    for m in {{ modules }}; do (cd $m && go test -race -shuffle=on ./...) || exit 1; done

# generate and compile client types for all served examples (Node.js/npm required)
client-types:
    ./tools/check_client_types.sh

# build every package
build:
    for m in {{ modules }}; do (cd $m && go build ./...) || exit 1; done

# run the full test suite
test:
    for m in {{ modules }}; do (cd $m && go test ./...) || exit 1; done

# report every lint and format finding; changes nothing
lint:
    {{ lintbin }} config verify
    for m in {{ modules }}; do (cd $m && {{ lintbin }} run ./...) || exit 1; done

# repair: sync go.mod, format, and apply every auto-fix
tidy:
    for m in {{ modules }}; do (cd $m && go mod tidy && {{ lintbin }} fmt ./... && {{ lintbin }} run --fix ./...) || exit 1; done

# format only (gofumpt, through the same pinned linter)
fmt:
    for m in {{ modules }}; do (cd $m && {{ lintbin }} fmt ./...) || exit 1; done

# serve the compact onboarding example
demo:
    go -C examples run ./onboarding

# export the spec to stdout (CI golden uses this same path)
spec:
    GO_SPEC_ONLY=1 go -C examples run ./onboarding

# regenerate the onboarding golden after intentional spec changes
golden:
    UPDATE_GOLDEN=1 go -C examples test -run TestGoldenSpec ./onboarding

# tidy, lint, test — the pre-commit sweep
sweep: tidy lint test
