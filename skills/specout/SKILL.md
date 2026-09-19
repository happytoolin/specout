---
name: specout
description: Add, migrate, or verify OpenAPI 3.1 documentation for Go HTTP services with github.com/happytoolin/specout. Use for typed handlers, router adapters, request and response schemas, served specifications, and route or status drift tests. Do not use for another OpenAPI library unless the user asks to migrate to specout.
---

# specout

Use specout to describe an existing Go HTTP API without moving request decoding,
validation, or response writing out of its handlers.

## Start from the application

Read the router construction, middleware order, route registrations, handler
signatures, and tests before editing. Preserve runtime behavior.

Use the installed module version as the API source of truth:

```sh
go doc github.com/happytoolin/specout
go doc github.com/happytoolin/specout/recorder
```

If the module is absent and the user asked to adopt specout, add it with
`go get github.com/happytoolin/specout@latest`. Confirm that the selected
release supports the Go version in `go.mod`.

## Describe the API

Create one generator per API:

```go
doc := specout.New(specout.Config{
	Title:   "Inventory API",
	Version: "1.0.0",
})
```

Wrap each existing `http.HandlerFunc` with its wire request and response types.
Keep the handler body unchanged.

```go
type listRequest struct {
	Limit int `query:"limit" jsonschema:"minimum=1,maximum=100,default=20"`
}

type page struct {
	Items []item `json:"items"`
}

h := specout.Handler[listRequest, page]{
	HandlerFunc: listItems,
	Summary:     "List items",
}
```

Model the actual wire contract:

- Use `query`, `path`, `header`, and `cookie` tags for parameters.
- Use `json` fields for the request body. Parameter fields are removed from it.
- Use `jsonschema` tags for constraints, defaults, formats, examples, and descriptions.
- Use `specout.File` for uploads and `specout.NoContent` for an empty response.
- Add each status the handler can return with `specout.Response`.
- Use `Config.ErrorType` and `Config.DefaultErrors` only for errors shared by most routes.
- Use `WithPublic` only when the route is exempt from `Config.Auth`.

A non-empty response type declares `200` by default. An empty response type
declares `204` when there is no explicit response. Explicit response entries
add or replace status declarations. Match them to the handler's real behavior.

## Register routes

Use the adapter for the application's current router:

```go
routes := specout.Chi(doc, router)
routes.Get("/items", h)
```

Use `specout.Gorilla` for gorilla/mux or `specout.Std` for http.ServeMux.

For an unsupported router, keep its existing registration and call
`specout.Document` with the same method and absolute path.

Serve the generator as an `http.Handler` at the application's chosen spec
endpoint. Register every documented route before the first `ServeHTTP` or
`WriteJSON` call because the first build freezes the document.

For chi and gorilla, call `Adopt` after route setup to reject undocumented
routes and resolve nested routes. Use `specout.Skip` only for intentionally
undocumented endpoints. `http.ServeMux` cannot enumerate routes, so register
all documented endpoints through `specout.Std`.

## Verify the contract

Add the smallest checks that protect the integration:

```go
func TestRoutesAreDocumented(t *testing.T) {
	doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
	router := chi.NewRouter()
	routes := specout.Chi(doc, router)
	registerRoutes(routes)
	routes.RequireDocumented(t)
}
```

When tests already exercise the live handlers, wrap the router and compare the
observed statuses with the declarations:

```go
recorded := recorder.New(router, specout.Skip("/openapi.json"))
// Exercise recorded with the existing HTTP tests.
recorder.Verify(t, doc, recorded)
```

Run `go test ./...`. Also run the repository's existing lint and OpenAPI
validation commands. Do not add a new validator or snapshot system only for
this change.

Use the [README](https://github.com/happytoolin/specout/blob/main/README.md)
and [onboarding example](https://github.com/happytoolin/specout/tree/main/examples/onboarding)
for current end-to-end patterns.
