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
```

If the module is absent and the user asked to adopt specout, add it with
`go get github.com/happytoolin/specout@latest`. Confirm that the selected
release supports the Go version in `go.mod`.

## Read the relevant references

- Read [types and responses](references/types-and-responses.md) when modeling
  parameters, bodies, schemas, files, unions, or response statuses.
- Read [configuration](references/configuration.md) when setting API metadata,
  shared errors, authentication schemes, operation metadata, or closed schemas.
- Read [routers and verification](references/routers-and-verification.md) before
  wiring chi, gorilla/mux, http.ServeMux, another router, or contract tests.
- For a complete new integration, read all three references.

## Integration shape

Create one generator per API. Wrap each existing `http.HandlerFunc` with the
request and response types that match its wire contract. Register it through
the adapter for the existing router.

```go
doc := specout.New(specout.Config{
	Title:   "Inventory API",
	Version: "1.0.0",
})

routes := specout.Chi(doc, router)
routes.Get("/items", specout.Handler[listRequest, page]{
	HandlerFunc: listItems,
	Summary:     "List items",
})
```

Serve `doc` as an `http.Handler` at the application's chosen OpenAPI endpoint.
Register every route, union variant, and schema name before the first
`ServeHTTP` or `WriteJSON` call. The first build freezes the document.

For chi and gorilla/mux, call `Adopt` on the outermost router after route setup.
Use `RequireDocumented` in tests when route coverage matters. http.ServeMux
cannot enumerate routes, so register documented endpoints through
`specout.Std`. Use `specout.Document` for other routers.

## Finish

Run `go test ./...`. Run the repository's existing lint and OpenAPI validation
commands. Add recorder verification only when tests already exercise live HTTP
handlers. Do not add a new validator or snapshot system only for this change.

Use the [project README](https://github.com/happytoolin/specout/blob/main/README.md)
and [onboarding example](https://github.com/happytoolin/specout/tree/main/examples/onboarding)
for current end-to-end patterns.
