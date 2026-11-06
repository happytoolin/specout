# Examples

All runnable examples use the same shape: `New` builds the generator and API
handler, `Handler` adds Swagger UI, and `main` delegates export and server
startup to `internal/examplekit`.

All specout code is in the route, type, and response files of each example. You
can copy it. `internal/examplekit` holds only plumbing that is not part of
specout: a JSON writer and reader, the Swagger UI page, and the server start.
Use your own helpers for these in your service.

| Example | Purpose | Router | Port |
|---|---|---|---|
| `onboarding` | Compact native example and golden document | chi | 8080 |
| `petstore` | Swagger Petstore, 13 paths and 19 operations | chi | 8081 |
| `msgraph` | Microsoft Graph subset, 5 paths and 8 operations | gorilla/mux | 8082 |

The examples are one Go module in `examples/`, so the root module does not
depend on chi or gorilla/mux. Run one from the `examples` directory:

```sh
cd examples
go run ./onboarding
go run ./petstore
go run ./msgraph
```

Each example serves Swagger UI at `/` and its generated document at
`/openapi.json`. Petstore uses its declared `/api/v3` server prefix.

Export a document without starting a server:

```sh
GO_SPEC_ONLY=1 go run ./onboarding
```

The example tests check published paths and shapes, drive real HTTP handlers
through `recorder.Verify`, and validate all generated documents in `just
validate`.

`Req` contains path, query, header, cookie, and body fields. Specout removes
parameter fields from the request body. `specoutchi.Check` and
`specoutgorilla.Check` scan the outermost router for undocumented routes. The
chi and gorilla/mux tests record statuses through the `Record` middleware of
the adapter. Document order follows registration order.
