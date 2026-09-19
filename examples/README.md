# Examples

All runnable examples use the same shape: `New` builds the generator and API
handler, `Handler` adds Swagger UI, and `main` delegates export and server
startup to `internal/examplekit`.

| Example | Purpose | Router | Port |
|---|---|---|---|
| `onboarding` | Compact native example and golden document | chi | 8080 |
| `petstore` | Swagger Petstore, 13 paths and 19 operations | chi | 8081 |
| `msgraph` | Microsoft Graph subset, 5 paths and 8 operations | gorilla/mux | 8082 |

Run one:

```sh
go run ./examples/onboarding
go run ./examples/petstore
go run ./examples/msgraph
```

Each example serves Swagger UI at `/` and its generated document at
`/openapi.json`. Petstore uses its declared `/api/v3` server prefix.

Export a document without starting a server:

```sh
GO_SPEC_ONLY=1 go run ./examples/onboarding
```

The example tests check published paths and shapes, drive real HTTP handlers
through `recorder.Verify`, and validate all generated documents in `just
validate`.

`Req` contains path, query, header, cookie, and body fields. Specout removes
parameter fields from the request body. `Adopt` scans the outermost chi or
gorilla router for undocumented routes. Document order follows registration
order.
