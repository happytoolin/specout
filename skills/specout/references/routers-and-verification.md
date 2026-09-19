# Routers and verification

Use this reference to select an adapter, serve the document, and add optional
route or status verification.

## Supported routers

| Router | Adapter | Route discovery |
|---|---|---|
| `github.com/go-chi/chi/v5` | `specout.Chi` | `Adopt` walks the outermost router and resolves groups and mounts. |
| `github.com/gorilla/mux` | `specout.Gorilla` | `Adopt` walks route templates and subrouters. |
| `net/http.ServeMux` | `specout.Std` | It cannot enumerate routes. Register documented endpoints through the adapter. |
| Other routers | `specout.Document` | Records metadata only. Runtime registration stays with the router. |

Verb helpers support `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`,
and `TRACE`. `specout.Std.Handle` and `specout.Document` accept the same OpenAPI
method set.

## chi/v5

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
router := chi.NewRouter()
routes := specout.Chi(doc, router)

routes.Get("/items", listItems)
routes.Post("/items", createItem)
router.Mount("/openapi.json", doc)

if err := routes.Adopt(); err != nil {
	return err
}
```

For grouped routes, register against the group and adopt only the outermost
router:

```go
router.Route("/v1", func(r chi.Router) {
	routes := specout.Chi(doc, r)
	routes.Get("/items", listItems)
})

if err := specout.Chi(doc, router).Adopt(); err != nil {
	return err
}
```

`Adopt` reports plain chi endpoints that do not have a matching specout
registration or `Document` declaration. It also gives the generator the full
paths for relative group registrations.

## gorilla/mux

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
router := mux.NewRouter()
api := router.PathPrefix("/v1").Subrouter()
routes := specout.Gorilla(doc, api)

routes.Get("/items/{id:[0-9]+}", getItem)
router.Handle("/openapi.json", doc).Methods(http.MethodGet, http.MethodHead)

if err := specout.Gorilla(doc, router).Adopt(); err != nil {
	return err
}
```

Adopt the outermost router so subrouter prefixes are visible. Endpoint routes
must have a method constraint. Path-only subrouter mounts are ignored.

Regex constraints in chi and gorilla templates are removed from the OpenAPI
path. For example, `/items/{id:[0-9]+}` becomes `/items/{id}`.

## http.ServeMux

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
mux := http.NewServeMux()
routes := specout.Std(doc, mux)

routes.Get("/items/{id}", getItem)
routes.Handle("POST /items", createItem)
mux.Handle("/openapi.json", doc)
```

There is no `Adopt` or `RequireDocumented` for http.ServeMux because it cannot
enumerate its route table. Direct calls to `mux.Handle` or `mux.HandleFunc` are
invisible to specout. Use `specout.Std` for each documented endpoint.

## Other routers

`Document` adds metadata without changing runtime routing. Use an absolute
OpenAPI path and register the real handler separately:

```go
h := specout.Handler[getItemRequest, item]{
	HandlerFunc: getItem,
	Summary:     "Get an item",
}

router.Handle(http.MethodGet, "/items/{id}", h.HandlerFunc)
specout.Document(doc, http.MethodGet, "/items/{id}", h)
```

Adapt the runtime registration line to the router's API. The path passed to
`Document` must start with `/` and must use OpenAPI `{name}` placeholders.

`Document` does not register, wrap, or serve the handler. It only records the
contract. A duplicate method and canonical path fails the document build.

## Paths that OpenAPI cannot express

Trailing catch-all routes such as `/files/*` and `/files/{path...}` are kept in
route identity checks but are omitted from the OpenAPI paths object. Do not
present them as normal documented operations.

OpenAPI also forbids equivalent templates with different parameter names,
such as `/items/{id}` and `/items/{name}`. specout fails the build instead of
emitting an ambiguous document.

## Serving and exporting the document

`Generator` is an `http.Handler`. It serves `GET` and `HEAD` and returns `405`
for other methods.

Export the deterministic JSON when a file or golden check is required:

```go
var out bytes.Buffer
if err := doc.WriteJSON(&out); err != nil {
	return err
}
```

The first `ServeHTTP` or `WriteJSON` call builds and freezes the document.
Registering a route, schema name, or union variant after that point panics.

## Route coverage

For chi and gorilla/mux, `RequireDocumented` turns an `Adopt` error into a test
failure:

```go
func TestRoutesAreDocumented(t *testing.T) {
	doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
	router := chi.NewRouter()
	routes := specout.Chi(doc, router)
	registerRoutes(routes)
	routes.RequireDocumented(t, specout.Skip("/healthz"))
}
```

`specout.Skip` accepts an exact path or a trailing wildcard such as
`/debug/*`. Skip only endpoints that are intentionally outside the API.

## Status verification with recorder

Recorder is optional. Use it when existing HTTP tests can send requests through
a wrapped chi, gorilla/mux, or http.ServeMux router.

```go
func TestObservedStatusesMatch(t *testing.T) {
	doc, router := newAPI()
	recorded := recorder.New(router, specout.Skip("/openapi.json"))

	recorded.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/items", nil),
	)
	// Exercise every declared route and route-specific status through recorded.

	recorder.Verify(t, doc, recorded)
}
```

`Verify` checks both directions:

- A route-specific status declared by the spec must be observed by a test.
- A status observed from a matched route must be allowed by the spec.

Global `DefaultErrors` are allowed but are not required on every route. A bare
`default` or response range allows matching statuses without creating a test
coverage requirement. HEAD and GET observations can satisfy each other.
Unmatched requests are not recorded.

Every relevant request must pass through the `Recorder`. Do not wrap only the
final assertion. Recorder cannot recover matched route templates from routers
that use `Document` as their only integration. Do not use it for those routers
unless their runtime adapter exposes a supported matched pattern.

## Minimum checks

Run the repository's existing commands. The normal minimum is:

```sh
go test ./...
```

Also run existing lint, OpenAPI validation, and client-generation checks when
the repository already has them. Add a golden OpenAPI file only when stable
spec review is a project requirement.
