# specout

Generate OpenAPI 3.1 from plain Go HTTP handlers.

`specout` keeps your runtime code native. It reads request and response types when you register routes, builds one deterministic OpenAPI document, and lets tests detect route or status-code drift.

| | |
|---|---|
| Output | OpenAPI 3.1 and JSON Schema 2020-12 |
| Go | 1.27 or later |
| Routers | `net/http`, `chi/v5`, and `gorilla/mux` |
| Runtime scope | Documentation only; your handlers keep control of decoding, validation, and responses |

## Why specout

- Keep normal `http.HandlerFunc` code.
- Define the contract with Go types and field tags.
- Generate stable JSON for review and version control.
- Compare declared routes with the live router.
- Compare declared responses with statuses observed in tests.
- Avoid comments, generators, and a second routing system.

## Install

```bash
go get github.com/happytoolin/specout
```

Install the public agent skill when you want an agent to add or verify specout
in a Go service:

```bash
npx skills add happytoolin/specout --skill specout
```

## Quick start

Define request and response shapes with ordinary Go types.

```go
type ListRequest struct {
	Limit int `query:"limit" jsonschema:"minimum=1,maximum=100,default=20"`
}

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Page struct {
	Items []Item `json:"items"`
}

var listItems = specout.Handler[ListRequest, Page]{
	HandlerFunc: serveItems,
	Summary:     "List items",
}
```

Register the handler through a router adapter.

```go
doc := specout.New(specout.Config{
	Title:   "Inventory API",
	Version: "1.0.0",
})

router := chi.NewRouter()
routes := specout.Chi(doc, router)
routes.Get("/items", listItems)

router.Mount("/openapi.json", doc)

if err := routes.Adopt(); err != nil {
	log.Fatal(err)
}

log.Fatal(http.ListenAndServe(":8080", router))
```

The service now exposes its specification at `GET /openapi.json`.

## Describe the contract

Most metadata comes from types and tags.

| Need | Declaration |
|---|---|
| Query parameter | `` `query:"limit"` `` |
| Path parameter | `` `path:"id"` `` |
| Header | `` `header:"X-Trace-ID"` `` |
| Cookie | `` `cookie:"session"` `` |
| JSON body | `` `json:"name"` `` |
| Optional property | `omitempty` or `omitzero` in its `json` tag |
| Nullable property | Pointer type |
| Constraints | `jsonschema` tags such as `minimum`, `maximum`, `pattern`, and `enum` |
| Custom request media type | `RequestContentTypes` on the handler |
| Binary upload | `specout.File` |
| Binary download | `ContentType` on a `specout.Response` |
| Empty response | `specout.NoContent` |
| Discriminated union | `doc.Register[T]("name")` and a `oneof_type` tag |
| Security | `Config.Auth` with `specout.Bearer`, `specout.APIKey`, or a custom scheme |
| Explicit schema name | `doc.SchemaName[T]("Name")` |

Use fluent methods when they make route declarations easier to read.

```go
h := specout.Get[Page]{HandlerFunc: servePage}.
	WithSummary("List items").
	WithTags("inventory")
```

Duplicate component names with different schemas fail when the document builds. Use an explicit schema name only when two Go types would otherwise collide.

## Declare responses

Success responses are inferred from the handler response type. Add other outcomes directly to the handler.

```go
h := specout.Handler[CreateRequest, Item]{
	HandlerFunc: createItem,
	Summary:     "Create an item",
	Responses: []specout.Response{
		{Status: http.StatusCreated, Headers: []specout.Header{{Name: "Location"}}},
		{Status: http.StatusBadRequest, Type: Problem{}},
		{Status: http.StatusConflict, Type: Problem{}},
	},
}
```

You can also set shared default problems in `specout.Config`.

## Verify the contract

Check that every live route has documentation.

```go
func TestRoutesAreDocumented(t *testing.T) {
	doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
	router := chi.NewRouter()
	routes := specout.Chi(doc, router)

	registerRoutes(routes)
	routes.RequireDocumented(t)
}
```

Check that tests observed only declared statuses, and that each declared status was observed.

```go
func TestObservedStatusesMatch(t *testing.T) {
	doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
	router := chi.NewRouter()
	registerRoutes(specout.Chi(doc, router))

	recorded := recorder.New(router, specout.Skip("/openapi.json"))
	// Exercise the API through recorded.

	recorder.Verify(t, doc, recorded)
}
```

## Router support

| Router | Adapter | Behavior |
|---|---|---|
| `chi/v5` | `specout.Chi` | Registers typed routes and can adopt routes from the live router |
| `gorilla/mux` | `specout.Gorilla` | Registers typed routes and can adopt routes from the live router |
| `http.ServeMux` | `specout.Std` | Registers typed routes explicitly |
| Other routers | `specout.Document` | Builds the document without a router adapter |

`chi` and `gorilla/mux` can enumerate their route trees. `http.ServeMux` cannot, so direct calls to `Handle` or `HandleFunc` are not visible to `specout`.

## How it works

```text
Handler[Request, Response]
        ↓
router adapter → route record → schema reflection → deterministic OpenAPI JSON
        ↓
test recorder → declared and observed status comparison
```

The document stays mutable while routes are registered. The first call to `WriteJSON` or `ServeHTTP` builds and freezes it. Later registration panics with a clear error.

## Examples and development

The canonical example is in [`examples/onboarding`](examples/onboarding). Its committed [`openapi.json`](examples/onboarding/openapi.json) is the sample specification and a golden test fixture.

```bash
just test          # Run the test suite.
just validate      # Validate the generated OpenAPI document.
just client-types  # Generate and compile client types from every example.
just demo          # Run the onboarding API on localhost:8080.
```

Two smaller integrations are also available:

- [`examples/petstore`](examples/petstore)
- [`examples/msgraph`](examples/msgraph)

## Design guarantees

- Generated output is deterministic.
- Schema names are stable.
- Route adoption finds undocumented paths and methods.
- Status verification checks both missing and unexpected outcomes.
- The library does not decode requests or write responses for you.
- The library emits OpenAPI 3.1 only.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
