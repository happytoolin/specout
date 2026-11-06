---
name: specout
description: Add, migrate, or verify OpenAPI 3.1 documentation for Go HTTP services with github.com/happytoolin/specout. Use for typed handlers, router adapters, request and response schemas, served specifications, and route or status drift tests. Do not use for another OpenAPI library unless the user asks to migrate to specout.
---

# specout

Use specout to describe an existing Go HTTP API without moving request decoding,
validation, authentication, or response writing out of its handlers.

## Inspect first

Read the router construction, middleware order, route registrations, handler
behavior, wire types, and HTTP tests before editing. Preserve runtime behavior.
Document what the service does, including every status that its handlers write.

Use the installed module version as the API source of truth:

```sh
go doc github.com/happytoolin/specout
go doc github.com/happytoolin/specout/recorder
go doc github.com/happytoolin/specout/adapters/specoutchi
```

If the module is absent and the user asked to adopt specout, add it with
`go get github.com/happytoolin/specout@latest`. Each router adapter is a
separate module. Add the adapter for the router, for example
`go get github.com/happytoolin/specout/adapters/specoutchi@latest`. Confirm
that the selected release supports the Go version in `go.mod`.

## Read the relevant references

- Read [types and responses](references/types-and-responses.md) when modeling
  parameters, bodies, schemas, files, unions, or response statuses.
- Read [configuration](references/configuration.md) when setting API metadata,
  shared errors, authentication schemes, operation metadata, or closed schemas.
- Read [routers and verification](references/routers-and-verification.md) before
  wiring chi, gorilla/mux, http.ServeMux, Echo, Gin, Fiber, another router, or
  contract tests.
- Read [OpenAPI coverage](references/openapi-coverage.md) to find how to set a
  specific OpenAPI field, or whether specout can write it.
- For a complete new integration, read the first three references.

## Integration shape

Create one generator per API. Wrap each existing `http.HandlerFunc` with the
request and response types that match its wire contract. Register it through
the adapter for the existing router.

```go
doc := specout.New(specout.Config{
	Title:   "Inventory API",
	Version: "1.0.0",
})

routes := specout.Bind(doc, specoutchi.New(router))
routes.Get("/items", specout.Handler[listRequest, page]{
	HandlerFunc: listItems,
	Summary:     "List items",
})
```

`specout.Bind` takes the adapter of the router, and the `Binder` that it
returns has the same methods on every router. Paths use the syntax of the
router. Put route middleware on a router group, and make the adapter from that
group.

Register every route, union variant, and schema name before the first
`Build` call. The first successful build freezes the document. Serve the bytes
from `Build` at the application's chosen OpenAPI endpoint.

The adapters are in `adapters/`: `specoutstd` (http.ServeMux, in the root
module), `specoutchi`, `specoutgorilla`, `specoutecho` (Echo v5),
`specoutechov4` (Echo v4), `specoutgin`, and `specoutfiber` (Fiber v3). After
route setup, call `specout.Check` with the adapter of the root router, engine,
or app. Give it the paths to skip, as in
`specout.Check(doc, specoutchi.New(router), "/openapi.json")`. In tests, write
`require.NoError(t, specout.Check(doc, specoutgin.New(engine), "/openapi.json"))`
when route coverage matters. A chi router does not know its prefix, so inside a
chi `Route` or `Mount`, pass the prefix: `specoutchi.New(r, "/v1")`.
http.ServeMux cannot list its routes, so `Check` returns an error for it. Use
the recorder for http.ServeMux. For other routers, implement `specout.Router`,
or use `doc.Document`.

## Finish

Call `doc.Build()` in a test, so that a bad document fails the test and not the
first request. Run `go test ./...`. Run the repository's existing lint and
OpenAPI validation commands. Add recorder verification only when tests already exercise live HTTP
handlers. Do not add a new validator or snapshot system only for this change.

Use the [project README](https://github.com/happytoolin/specout/blob/main/README.md)
and [onboarding example](https://github.com/happytoolin/specout/tree/main/examples/onboarding)
for current end-to-end patterns.
