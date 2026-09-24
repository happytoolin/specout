# Code guide

specout documents HTTP handlers. The handlers and their routers control request
handling. The library collects metadata, builds a document, and checks route
coverage.

## Follow one route

1. `Handler[Req, Res]` holds the types and operation metadata.
2. A router adapter creates a `routeRecord`. `Document` records routes that use
   other routers.
3. `Adopt` checks a live router for undocumented routes. Chi also uses the router
   walk to resolve group and mount prefixes.
4. The first `WriteJSON` or `ServeHTTP` call resolves routes and builds operations.
   Each operation adds parameters, a request body, and responses.
5. The schema registry reflects types and collects components. It resolves union
   variants before it assigns names to generated union branches.
6. A successful build stores the JSON document. That stored document also marks
   the generator as frozen. A failed build can be retried.

## Where to change code

| Area | Files |
| --- | --- |
| Public types and registration state | `specout.go`, `handler.go`, `config.go`, `response.go` |
| Router binding and route identity | `register_*.go`, `document.go`, `routes.go` |
| Path normalization and route checks | `paths.go`, `stray.go`, `verify.go` |
| Document and operation assembly | `build.go`, `operation.go`, `security.go` |
| Parameter metadata and styles | `params.go` |
| JSON field selection and request bodies | `fields.go`, `body.go`, `params_body.go` |
| Component reflection and names | `schema.go`, `defs.go` |
| Inline property reflection | `schema_inline.go` |
| Schema traversal and corrections | `walk.go`, `fixups.go` |
| Discriminated union generation | `unions.go` |
| Response plans and status expectations | `responses.go`, `statusmap.go` |
| Ordered JSON output and serving | `ordered.go`, `emit.go`, `serve.go` |
| Observed HTTP status checks | `recorder/` |

Response generation and status checks use the same response plan. Change that
plan when response precedence changes. Keep router-specific matching in the
adapters and recorder. The routers have different rules for mounts, wildcards,
and HEAD requests.

## Check a change

Run `go test ./...` during development. Run `just check` before submitting a
change. It includes race checks, generated schema validation, invalid payload
checks, client type compilation, and vulnerability scans.

Keep regression tests when simplifying code. Check the emitted document as well
as the Go values: JSON field names, nullability, required fields, and response
status ranges are part of the contract. Examples remain ordinary HTTP handlers;
shared example helpers belong in `internal/examplekit`.
