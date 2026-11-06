# Code guide

specout documents HTTP handlers. The handlers and their routers control request
handling. The library collects metadata, builds a document, and checks route
coverage.

## Follow one route

1. `Handler[Req, Res]` holds the types and operation metadata.
2. `Binder.Handle` in `router.go` creates a `routeRecord`. The `Binder` comes
   from `specout.Bind`, which takes the adapter of a router: a
   `specout.Router`. `Document` records routes that use other routers.
   `addRoute` checks the responses, `Raw` keys, and media types before the
   route reaches the router, so a bad route panics at its registration and
   leaves no record. The `Add` method of the adapter adds the live route and
   returns its full path, so specout knows the path at registration.
3. `specout.Check` reads the live route table from the `Routes` method of the
   adapter. The result names the routes that have no documentation.
4. The first `Build` call resolves routes and builds operations.
   Each operation adds parameters, a request body, and responses.
5. The schema registry reflects types and collects components. It resolves union
   variants before it assigns names to generated union branches.
6. A successful build stores the JSON document. That stored document also marks
   the generator as frozen. A failed build stores nothing, so the next `Build`
   runs again.

## Where to change code

| Area | Files |
| --- | --- |
| Public types and registration state | `specout.go`, `handler.go`, `config.go`, `response.go`, `enum.go` |
| Requests your API sends, and links | `callbacks.go`, `links.go` |
| Request encodings and examples | `encoding.go`, `examples.go` |
| Router binding and route identity | `router.go`, `bind.go`, `document.go`, `routes.go` |
| Hooks for the adapters and the recorder | `hooks.go`, `internal/bridge` |
| Router adapters | `adapters/specoutstd` (in the root module), `adapters/specoutchi`, `adapters/specoutgorilla`, `adapters/specoutecho`, `adapters/specoutechov4`, `adapters/specoutgin`, `adapters/specoutfiber` |
| Shared adapter code | `internal/echopath`, `adapters/specoutfiber/internal/fiberpath` (Fiber path translation), `internal/adaptertest` (test helpers) |
| Path normalization and route checks | `paths.go`, `bind.go` |
| Document and operation assembly | `build.go`, `operation.go`, `security.go` |
| Parameter metadata and styles | `params.go` |
| `jsonschema` tag checks | `schema_tags.go` |
| Checks that keep the document valid OpenAPI 3.1 and JSON Schema 2020-12 | `schema_values.go`, `request_checks.go`, `raw_checks.go` |
| Message form and typo hints | `errors.go` |
| JSON field selection and request bodies | `fields.go`, `body.go`, `params_body.go` |
| Component reflection and names | `schema.go`, `defs.go` |
| Inline property reflection | `schema_inline.go` |
| Schema traversal and corrections | `walk.go`, `fixups.go` |
| Discriminated union generation | `unions.go` |
| Response plans and status expectations | `responses.go`, `statusmap.go` |
| Ordered JSON output and serving | `ordered.go`, `emit.go`, `serve.go` |
| Observed HTTP status checks | `recorder/` (`match.go` finds the route of a request that the router table did not name), `internal/hijack` (status of a hijacked connection) |

Response generation and status checks use the same response plan. Change that
plan when response precedence changes. Keep router-specific matching in the
adapters and recorder. The routers have different rules for mounts, wildcards,
and HEAD requests.

## Modules

The repository has eight Go modules: the root module, one module for each of
the six router adapters in `adapters/`, and the examples module in
`examples/`. The `adapters/specoutstd` package has no router dependency, so it
is part of the root module. Each adapter `go.mod` replaces `github.com/happytoolin/specout`
with `../..`, so the adapters build against the code in this repository. The
examples `go.mod` replaces the root module and the chi and gorilla/mux adapters
with their directories in this repository. Thus the root module does not
depend on a router. The `just` recipes run in every module.

To release:

1. Tag the root module, for example `v0.1.0`.
2. Set the `github.com/happytoolin/specout` requirement of each adapter
   `go.mod` and of `examples/go.mod` to that version, and run `just tidy`.
3. Tag each adapter with its directory, for example
   `adapters/specoutgin/v0.1.0`. Do not tag the examples module.

## Check a change

Run `just test` during development. `go test ./...` in the root does not test
the adapter modules or the examples. Run `just check` before submitting a change. It includes
race checks in random test order, generated schema validation, invalid payload
checks, client type compilation, and vulnerability scans.

`just lint` runs golangci-lint with every linter enabled. The config lists each
disabled linter or rule with its reason; a `nolint` comment must name the
linter and give a reason. Library code has firm size limits: cyclomatic
complexity 10, cognitive complexity 15, 50 lines or 35 statements per function,
three levels of control nesting, and five arguments. When a function goes over
a limit, split it into named steps. Do not raise the limit.

`just nilaway` runs [NilAway](https://github.com/uber-go/nilaway) on library
and example code, and fails on any nil flow that can panic. Fix the code so the
flow is guarded. NilAway has no suppression comment. It skips test
files, because it cannot see that a testify `require` call stops the test.

Each panic and error has one form: `specout: <where>: <problem>; <fix>`. Put the
place first: a route such as `POST /jobs: response 200`, or a Go name such as
`Config.Auth`. Quote the values that the user wrote. Give the fix when specout
knows it, and use `didYouMean` when the value can be a typo. A check that cannot
run at registration goes in the build, and the build returns every problem with
`errors.Join`.

`skills/specout/references/openapi-coverage.md` lists every field of the
official OpenAPI 3.1 schema in `testdata/openapi-3.1-schema.json`.
`TestOpenAPICoverage` fails when the table and the code disagree, so update the
table when you add a field.

Put a new test in the file for its topic:

| Topic | Test file |
| --- | --- |
| Schema reflection, nullability, enums, and component names | `schema_test.go` |
| Discriminated unions | `union_test.go` |
| Parameters, request bodies, forms, and encodings | `params_test.go` |
| Responses, status codes, and examples | `responses_test.go` |
| Route binding, route checks, and path checks | `routes_test.go` |
| Document fields, configuration, and build errors | `document_test.go` |
| Webhooks, callbacks, and links | `outgoing_test.go` |
| Fuzz targets | `fuzz_test.go` |

Keep regression tests when simplifying code. Check the emitted document as well
as the Go values: JSON field names, nullability, required fields, and response
status ranges are part of the contract. Examples remain ordinary HTTP handlers;
shared example helpers belong in `internal/examplekit`.
