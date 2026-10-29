# Routers and verification

Use this reference to select an adapter, serve the document, and add optional
route or status verification.

## Supported routers

| Router | Adapter | Route discovery |
|---|---|---|
| `github.com/go-chi/chi/v5` | `specout.Chi` | `Adopt` walks the outermost router and resolves groups and mounts. |
| `github.com/gorilla/mux` | `specout.Gorilla` | `Adopt` walks route templates and subrouters. |
| `net/http.ServeMux` | `specout.Std` | It cannot enumerate routes. Register documented endpoints through the adapter. |
| `github.com/labstack/echo/v5` | `specoutecho.New` | `specoutecho.Adopt` reads the Echo route table. |
| `github.com/labstack/echo/v4` | `specoutechov4.New` | `specoutechov4.Adopt` reads the Echo route tables. |
| `github.com/gin-gonic/gin` | `specoutgin.New` | `specoutgin.Adopt` reads the Gin route table. |
| `github.com/gofiber/fiber/v3` | `specoutfiber.New` | `specoutfiber.Adopt` reads the Fiber route table. |
| `github.com/gofiber/fiber/v2` | `specoutfiberv2.New` | `specoutfiberv2.Adopt` reads the Fiber route table. |
| Other routers | `specout.Bind` or `specout.Document` | `Bind` registers and records. `Document` records metadata only. |

The Echo, Gin, and Fiber adapters are in separate packages, so the root package
does not import those frameworks:

```go
import (
	"github.com/happytoolin/specout/adapters/specoutecho"    // Echo v5
	"github.com/happytoolin/specout/adapters/specoutechov4"  // Echo v4
	"github.com/happytoolin/specout/adapters/specoutfiber"   // Fiber v3
	"github.com/happytoolin/specout/adapters/specoutfiberv2" // Fiber v2
	"github.com/happytoolin/specout/adapters/specoutgin"
)
```

In this reference, "Echo" means both Echo adapters and "Fiber" means both Fiber
adapters, except in the sections for one version.

Verb helpers support `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`,
and `TRACE`. `Handle`, `specout.Bind`, and `specout.Document` accept the same
OpenAPI method set.

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

`Adopt` on an inner router documents the paths relative to its mount. For
example, a router mounted at `/api` documents `/items`, and clients call
`/api/items`. When a spec request goes through chi, the build walks the
outermost router. A chi registration served under a mount prefix fails the
build, unless a `Config.Servers` URL ends with that prefix. A templated server
URL accepts every prefix.

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
must have a method constraint. Path-only subrouter mounts are ignored. So is a
route whose handler is another `*mux.Router`; `Adopt` walks that router next.

A route without a path matcher, such as `r.Methods("OPTIONS").Handler(cors)`,
serves every path. `Adopt` reports it as `OPTIONS *`. Skip it with
`specout.Skip("*")`.

A malformed template, such as `/items/{id`, panics at registration with the
gorilla error.

Regex constraints in chi and gorilla templates are removed from the OpenAPI
path. For example, `/items/{id:[0-9]+}` becomes `/items/{id}`. A gorilla
parameter whose regex can match `/`, such as `{path:.*}`, spans several
segments. specout treats that route as a catch-all.

## http.ServeMux

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
mux := http.NewServeMux()
routes := specout.Std(doc, mux)

routes.Get("/items/{id}", getItem)
routes.Handle("POST /items", createItem)
mux.Handle("/openapi.json", doc)
```

A path that ends in `/` without `{$}` matches every path below it, as
`/files/{path...}` does. specout treats it as a catch-all and omits it from the
paths object. Write `/{$}` for the root path alone, and `/items/{$}` for an
exact path with a trailing slash.

There is no `Adopt` or `RequireDocumented` for http.ServeMux because it cannot
enumerate its route table. Direct calls to `mux.Handle` or `mux.HandleFunc` are
invisible to specout. Use `specout.Std` for each documented endpoint.

## Echo v5

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
e := echo.New()
api := e.Group("/v1")
routes := specoutecho.New(doc, api)

routes.Get("/items/:id", getItem)
routes.Post("/items", createItem, requireAuth) // route middleware runs first
e.GET("/openapi.json", echo.WrapHandler(doc))

if err := specoutecho.Adopt(doc, e, specout.Skip("/openapi.json")); err != nil {
	return err
}
```

`specoutecho.New` accepts an `*echo.Echo` or an `*echo.Group`. Each route is
documented at the full path that Echo reports, with the group prefix. Echo
joins a group prefix and a route path without a slash, so `"/v1"` and `"items"`
give `/v1items`.

| Echo path | OpenAPI path |
|---|---|
| `/items/:id` | `/items/{id}` |
| `/items/:id\:cancel` | `/items/{id}:cancel` |
| `/files/*` | Catch-all. It is omitted from the paths object. |

The handler reads parameters with `r.PathValue("id")`. `echo.WrapHandler` copies
them from the Echo context. `Adopt` needs the `*echo.Echo`. It reports `Any`
routes as `(no method constraint)`. It ignores the 404 routes that Echo adds
for group middleware.

## Echo v4

The `specoutechov4` package has the same API as `specoutecho`:

```go
e := echo.New()
routes := specoutechov4.New(doc, e.Group("/v1"))
routes.Get("/items/:id", getItem)
e.GET("/openapi.json", echo.WrapHandler(doc))

if err := specoutechov4.Adopt(doc, e, specout.Skip("/openapi.json")); err != nil {
	return err
}
```

Echo v4 reads paths as Echo v5 does, so the path table above applies. These
differences remain:

- `echo.WrapHandler` in v4 does not set path values. The adapter copies them
  into `r.PathValue` itself.
- Echo v4 has no parameter list in its route table, so no router check
  confirms the translation at registration.
- A later route on the same path always replaces the earlier one. specout
  panics on the second registration, and the path then answers `404`.
- `Any` adds one route for each of 11 methods. `Adopt` reports them as one
  `(no method constraint)` route. `Adopt` also reads the routers of `e.Host`.
  A documented route documents its own router only. A host route that you
  register on Echo itself, on the method and path of a documented route of
  another router, is reported with its host, as `GET api.example.com/items`.
- `Record` records a request only when the route table has a route for its
  method. Echo v4 keeps the route path in the context for a `405` and for its
  automatic `OPTIONS` answer.

## Gin

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
engine := gin.New()
api := engine.Group("/v1")
routes := specoutgin.New(doc, api)

routes.Get("/items/:id", getItem)
routes.Post("/items", createItem, requireAuth) // route middleware runs first
engine.GET("/openapi.json", gin.WrapH(doc))

if err := specoutgin.Adopt(doc, engine, specout.Skip("/openapi.json")); err != nil {
	return err
}
```

`specoutgin.New` accepts a `*gin.Engine` or a `*gin.RouterGroup`. Gin cleans the
joined path, so `"/v1/"` and `"/items"` give `/v1/items`. A trailing slash on
the route stays.

| Gin path | OpenAPI path |
|---|---|
| `/items/:id` | `/items/{id}` |
| `/a\:b` | `/a:b` |
| `/files/*filepath` | Catch-all. It is omitted from the paths object. |

The adapter copies `c.Params` into `r.PathValue` before the handler runs. A
catch-all value drops the leading slash that Gin gives it, so `/files/a/b`
gives `a/b`, as `/files/{path...}` does on `http.ServeMux`. Gin decodes the
values unless the engine sets `UseRawPath` or `UseEscapedPath` and clears
`UnescapePathValues`. `Adopt` needs the
`*gin.Engine`. Gin's `Any` registers one route for each method, so `Adopt` also
reports its `CONNECT` route.

## Fiber v3

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
app := fiber.New()
api := app.Group("/v1")
routes := specoutfiber.New(doc, api)

routes.Get("/items/:id<int>", getItem)
routes.Post("/items", createItem, requireAuth) // route middleware runs first
app.Get("/openapi.json", adaptor.HTTPHandler(doc))

if err := specoutfiber.Adopt(doc, app, specout.Skip("/openapi.json")); err != nil {
	return err
}
```

`specoutfiber.New` accepts a `*fiber.App` or a `*fiber.Group`. It rejects a
domain router from `app.Domain`, because the host is not part of the path.
Fiber does not run net/http handlers directly. Each route runs its handler
through Fiber's `adaptor` package, which builds an `*http.Request` for each
call.

| Fiber path | OpenAPI path |
|---|---|
| `/items/:id` | `/items/{id}` |
| `/items/:id<int>` | `/items/{id}`. The `Req` field types the parameter. |
| `/flights/:from-:to` | `/flights/{from}-{to}` |
| `/files/*` and `/files/+` | Catch-all. It is omitted from the paths object. |
| `/users/:id?` | Panics. OpenAPI has no optional path parameter. Register both paths. |

The handler reads parameters with `r.PathValue`. Fiber names wildcards `*1`,
`*2`, and `+1`. Fiber answers `HEAD` for every `GET` route. The document lists
the `GET` operation only, like http.ServeMux. `Adopt` needs the `*fiber.App`. It
ignores middleware routes and the `HEAD` routes that Fiber adds for `GET`
routes. It reports a `HEAD` route that is registered on its own. It also compares each translated path with the parameters that Fiber
parsed, so a path that specout reads differently fails `Adopt`.

`Adopt` also reports a route that never runs, because an earlier route of its
method takes all of its requests, as `/users/:id` before `/users/me` or
`/files/*` before `/files/:name`. A guard route that calls `c.Next()` sends its
requests on to the later routes, so skip the guard with `specout.Skip`.

Register routes on the app that serves them. A sub-app mounted with `app.Use`
changes the path after registration. Use a group instead.

## Fiber v2

The `specoutfiberv2` package has the same API as `specoutfiber`, with
`*fiber.Ctx` handlers:

```go
app := fiber.New()
routes := specoutfiberv2.New(doc, app.Group("/v1"))
routes.Get("/items/:id<int>", getItem)
app.Get("/openapi.json", adaptor.HTTPHandler(doc))

if err := specoutfiberv2.Adopt(doc, app, specout.Skip("/openapi.json")); err != nil {
	return err
}
```

Fiber v2 reads paths as Fiber v3 does, so the path table above applies. These
differences remain:

- `Get` registers the `GET` route with Fiber's own `Get`, which adds the `HEAD`
  route at registration. `Adopt` does not report that `HEAD` route. It takes
  requests, so `Adopt` reports a later `HEAD /users/me` after
  `GET /users/:id`, which never runs.
- Fiber v2 appends a later route on the same path to the earlier one and runs
  the earlier one first. So a `Head` after a `Get` on the same path panics.
  Register `Head` first.
- Fiber v2 has no domain router.
## Path rules for Echo, Gin, and Fiber

Literal `{` and `}` in a route path panic at registration, because OpenAPI
reads braces as parameters. A path that the adapter cannot translate also
panics. Gin and Fiber keep no route for it. Echo shows the group prefix only
after it adds the route, so a bad Echo group prefix leaves a route that answers
`404`.

A second registration of the same method and path panics, also on routers that
allow it: `echo.New()` lets a later route replace an earlier one, and Fiber
never runs the second handler. After the panic, the Echo path answers `404`.

## Other routers

`Bind` registers a handler on any router and records its metadata. The `mount`
function adds the live route and returns the full path in OpenAPI syntax.
Write a small binder with it when a router can report the full path at
registration:

```go
specout.Bind(doc, http.MethodGet, "/items/:id", h, func(next http.Handler) string {
	router.Handler(http.MethodGet, "/items/:id", next)
	return "/items/{id}"
})
```

`mount` runs while the generator is locked, so it must not call the generator.
`doc.CheckRoutes` compares a route table, in OpenAPI syntax, with the
registered routes. It returns a `*specout.StrayError`.

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
Router syntax such as `/items/:id`, unpaired braces, `{$}`, and empty segments
(`//`) panic.

`Document` does not register, wrap, or serve the handler. It only records the
contract. A duplicate method and canonical path fails the document build.

## Paths that OpenAPI cannot express

Trailing catch-all routes such as `/files/*`, `/files/{path...}`, and the
http.ServeMux subtree `/files/` are kept in route identity checks but are
omitted from the OpenAPI paths object. Do not
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

A failed build does not freeze the document. `WriteJSON` returns the error.
`ServeHTTP` returns `500` with a message that names no route or type, and it
writes the error to the standard `log` package once. The build runs again only
after a registration, an `Adopt`, or a spec request through another chi
router. A route added to an adopted router alone does not start a new build.

## Route coverage

For chi, gorilla/mux, Echo, Gin, and Fiber, `RequireDocumented` turns an `Adopt`
error into a test failure:

```go
func TestRoutesAreDocumented(t *testing.T) {
	doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
	router := chi.NewRouter()
	routes := specout.Chi(doc, router)
	registerRoutes(routes)
	routes.RequireDocumented(t, specout.Skip("/healthz"))
}
```

For Echo, Gin, and Fiber, `RequireDocumented` is a package function:

```go
specoutgin.RequireDocumented(t, doc, engine, specout.Skip("/openapi.json"))
```

`specout.Skip` accepts an exact path or a trailing wildcard such as
`/debug/*`. Skip only endpoints that are intentionally outside the API. The
Echo, Gin, and Fiber route tables do not show which handler serves the spec, so
skip the spec endpoint.

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

Redirects that a router writes itself are not recorded. http.ServeMux and
gorilla/mux redirect to the cleaned path, and http.ServeMux and gorilla
`StrictSlash` add or remove a trailing slash. A route that redirects to its own
path with the slash changed looks the same, so it is not recorded either.

Global `DefaultErrors` are allowed but are not required on every route. A bare
`default` or response range allows matching statuses without creating a test
coverage requirement. With http.ServeMux, a HEAD request handled by a GET route
counts toward that GET route. An explicit HEAD route needs its own test.
Unmatched requests are not recorded.

Every relevant request must pass through the `Recorder`. Do not wrap only the
final assertion. Pass the router itself to `recorder.New`. A middleware
wrapper hides the routes; `Verify` then reports that no route matched.

For Echo, Gin, and Fiber, create the recorder without a router and install the
adapter's `Record` middleware:

```go
rec := recorder.New(nil, specout.Skip("/openapi.json"))
engine := gin.New()
engine.Use(specoutgin.Record(rec)) // before any route
registerRoutes(specoutgin.New(doc, engine))

engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items", nil))
recorder.Verify(t, doc, rec)
```

| Router | Install | Middleware responses |
|---|---|---|
| Echo | `e.Use(specoutecho.Record(rec))`. Order does not matter. | Recorded. |
| Gin | `engine.Use(specoutgin.Record(rec))` before any route. | Recorded. |
| Fiber | `app.Use(specoutfiber.Record(rec))` before any route. | Recorded. `Record` finds the endpoint with `fiber.RoutePatternMatch`. |

Echo and Fiber pass a returned error to the framework's error handler, so the
recorder sees the status that the client gets. Fiber records a `HEAD` request
to a `GET` route under `GET`.

`recorder.Observe` records one status for one route. Use it to feed the
recorder from another router's middleware. Recorder cannot recover matched
route templates from routers that use `Document` as their only integration.

## Minimum checks

Run the repository's existing commands. The normal minimum is:

```sh
go test ./...
```

Also run existing lint, OpenAPI validation, and client-generation checks when
the repository already has them. Add a golden OpenAPI file only when stable
spec review is a project requirement.
