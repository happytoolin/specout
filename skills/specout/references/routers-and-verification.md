# Routers and verification

Use this reference to select an adapter, serve the document, and add optional
route or status verification.

## One API on every router

`specout.Bind` takes the generator and the adapter of a router. It returns a
`*specout.Binder`, which has the same methods on every router: `Handle`, and
`Get`, `Head`, `Post`, `Put`, `Patch`, `Delete`, `Options`, and `Trace`. Only
the line that makes the adapter changes:

```go
api := specout.Bind(doc, specoutstd.New(mux))                  // http.ServeMux
api := specout.Bind(doc, specoutchi.New(r))                     // chi
api := specout.Bind(doc, specoutgorilla.New(r))                 // gorilla/mux
api := specout.Bind(doc, specoutecho.New(e.Group("/v1")))       // Echo v5
api := specout.Bind(doc, specoutechov4.New(e.Group("/v1")))     // Echo v4
api := specout.Bind(doc, specoutgin.New(engine.Group("/v1")))   // Gin
api := specout.Bind(doc, specoutfiber.New(app.Group("/v1")))    // Fiber v3

api.Get("/items/{id}", getItem) // the path uses the router's own syntax
```

Put route middleware on a router group, in the router's own way, and make the
adapter from that group: `engine.Group("/admin", requireAdmin)` on Gin, Echo,
and Fiber, `r.With(requireAdmin)` on chi, and `sub.Use(requireAdmin)` on a
gorilla subrouter.

`specout.Check(doc, adapter, skips...)` compares the route table of a router
with the document. Pass it the adapter of the root router.

## Supported routers

| Router | Adapter | Route discovery |
|---|---|---|
| `github.com/go-chi/chi/v5` | `specoutchi.New` | `Check` walks the chi router. |
| `github.com/gorilla/mux` | `specoutgorilla.New` | `Check` walks route templates and subrouters. |
| `net/http.ServeMux` | `specoutstd.New` | None. ServeMux cannot list its routes. Use the recorder. |
| `github.com/labstack/echo/v5` | `specoutecho.New` | `Check` reads the Echo route table. |
| `github.com/labstack/echo/v4` | `specoutechov4.New` | `Check` reads the Echo route tables. |
| `github.com/gin-gonic/gin` | `specoutgin.New` | `Check` reads the Gin route table. |
| `github.com/gofiber/fiber/v3` | `specoutfiber.New` | `Check` reads the Fiber route table. |
| Other routers | Your own `specout.Router`, or `doc.Document` | See [other routers](#other-routers). |

`specoutstd` is part of the root module. Each other adapter is a separate
module, so the root package does not import a router. Add the module of the
adapter, for example
`go get github.com/happytoolin/specout/adapters/specoutchi@latest`:

```go
import (
	"github.com/happytoolin/specout/adapters/specoutstd"     // http.ServeMux
	"github.com/happytoolin/specout/adapters/specoutchi"
	"github.com/happytoolin/specout/adapters/specoutecho"    // Echo v5
	"github.com/happytoolin/specout/adapters/specoutechov4"  // Echo v4
	"github.com/happytoolin/specout/adapters/specoutfiber"   // Fiber v3
	"github.com/happytoolin/specout/adapters/specoutgin"
	"github.com/happytoolin/specout/adapters/specoutgorilla"
)
```

In this reference, "Echo" means both Echo adapters, except in the sections for
one version.

`Handle` and `doc.Document` accept the OpenAPI methods: `GET`, `HEAD`,
`POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, and `TRACE`.

`Check` takes the paths to skip as strings. A skip is an exact path or a
trailing wildcard such as `/debug/*`.

## chi/v5

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
router := chi.NewRouter()
api := specout.Bind(doc, specoutchi.New(router))

api.Get("/items", listItems)
api.Post("/items", createItem)

if err := specout.Check(doc, specoutchi.New(router), "/openapi.json"); err != nil {
	return err
}
```

A chi router does not know the prefix where it is routed or mounted. Inside a
chi `Route` or `Mount`, pass the full prefix to `specoutchi.New`:

```go
router.Route("/v1", func(r chi.Router) {
	specout.Bind(doc, specoutchi.New(r, "/v1")).Get("/items", listItems)

	r.Route("/items/{id}", func(r chi.Router) {
		item := specout.Bind(doc, specoutchi.New(r.With(requireAdmin), "/v1/items/{id}"))
		item.Delete("/", deleteItem)
	})
})
```

A chi `Group` has no prefix, so `specoutchi.New(r)` inside it needs none. A
wrong prefix documents the wrong path. `Check` on the root router then reports
the real path as undocumented.

`Check` reports chi endpoints that do not have a matching specout registration
or `Document` declaration. Pass the path of the spec endpoint as a skip. A trailing `*`, as in
`/files/*`, is the catch-all `{*...}`, and the paths object leaves it out.

`specoutchi.Record(rec)` is chi middleware for the recorder. See
[status verification](#status-verification-with-recorder).

## gorilla/mux

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
router := mux.NewRouter()
api := specout.Bind(doc, specoutgorilla.New(router.PathPrefix("/v1").Subrouter()))

api.Get("/items/{id:[0-9]+}", getItem)

if err := specout.Check(doc, specoutgorilla.New(router), "/openapi.json"); err != nil {
	return err
}
```

Each route is documented at its full template, with the subrouter prefix.
Call `Check` on the outermost router, so that it sees every subrouter. Endpoint
routes must have a method constraint. Path-only subrouter mounts are ignored.
So is a route whose handler is another `*mux.Router`; `Check` walks that router
next. The handler reads gorilla route variables with `r.PathValue`.

A route without a path matcher, such as `r.Methods("OPTIONS").Handler(cors)`,
serves every path. `Check` reports it as `OPTIONS *`. Skip it with the skip
`"*"`.

A malformed template, such as `/items/{id`, panics at registration with the
gorilla error. gorilla has the route at that time.

Regex constraints in chi and gorilla templates are removed from the OpenAPI
path. For example, `/items/{id:[0-9]+}` becomes `/items/{id}`. A parameter
whose regex can match `/`, such as gorilla `{path:.*}` or chi
`{name:.+}`, is a normal path parameter in the document.

## http.ServeMux

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
mux := http.NewServeMux()
api := specout.Bind(doc, specoutstd.New(mux))

api.Get("/items/{id}", getItem)
api.Post("/items", createItem)
```

The adapter registers `"GET /items/{id}"` on the mux. A path that ends in `/`
without `{$}` matches every path below it, as `/files/{path...}` does. specout
treats it as a catch-all and omits it from the paths object. Write `/{$}` for
the root path alone, and `/items/{$}` for an exact path with a trailing slash.
`/items/` and `/items/{$}` are two routes.

http.ServeMux cannot list its routes, so `Check` returns an error for it.
Direct calls to `mux.Handle` or `mux.HandleFunc` are invisible to specout. Wrap
the mux with `recorder.New` to find them in tests.

## Echo v5

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
e := echo.New()
api := specout.Bind(doc, specoutecho.New(e.Group("/v1")))
admin := specout.Bind(doc, specoutecho.New(e.Group("/v1/admin", requireAuth)))

api.Get("/items/:id", getItem)
admin.Post("/items", createItem) // the group middleware runs first

if err := specout.Check(doc, specoutecho.New(e), "/openapi.json"); err != nil {
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
them from the Echo context. `Check` needs the adapter of the `*echo.Echo`. It
reports `Any`
routes as `(no method constraint)`. It ignores the 404 routes that Echo adds
for group middleware.

## Echo v4

The `specoutechov4` package has the same API as `specoutecho`:

```go
e := echo.New()
api := specout.Bind(doc, specoutechov4.New(e.Group("/v1")))
api.Get("/items/:id", getItem)

if err := specout.Check(doc, specoutechov4.New(e), "/openapi.json"); err != nil {
	return err
}
```

Echo v4 reads paths as Echo v5 does, so the path table above applies. These
differences remain:

- `echo.WrapHandler` in v4 does not set path values. The adapter copies them
  into `r.PathValue` itself, as Echo gives them. Echo v4 reads an encoded
  request path as it is, so `/users/a%40b` gives `a%40b`.
- Echo v4 has no parameter list in its route table, so no router check
  confirms the translation at registration.
- A later route on the same path always replaces the earlier one. specout
  panics on the second registration.
- `Any` adds one route for each of 11 methods, and `Check` reports each one.
  `Check` also reads the routers of `e.Host`, without the host in the path.
- `Record` records a request only when the route table has a route for its
  method. Echo v4 keeps the route path in the context for a `405` and for its
  automatic `OPTIONS` answer.

## Gin

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
engine := gin.New()
api := specout.Bind(doc, specoutgin.New(engine.Group("/v1")))
admin := specout.Bind(doc, specoutgin.New(engine.Group("/v1/admin", requireAuth)))

api.Get("/items/:id", getItem)
admin.Post("/items", createItem) // the group middleware runs first

if err := specout.Check(doc, specoutgin.New(engine), "/openapi.json"); err != nil {
	return err
}
```

`specoutgin.New` accepts a `*gin.Engine` or a `*gin.RouterGroup`. Gin cleans the
joined path, so `"/v1/"` and `"/items"` give `/v1/items`. A trailing slash on
the route stays.

| Gin path | OpenAPI path |
|---|---|
| `/items/:id` | `/items/{id}` |
| `/files/*filepath` | Catch-all. It is omitted from the paths object. |

Gin removes the `\:` escapes from its route table after the first request, so
the adapter rejects an escaped path at registration.

The adapter copies `c.Params` into `r.PathValue` before the handler runs, as
Gin gives them. A catch-all value keeps its leading slash, so `/files/a/b`
gives `/a/b`. Gin decodes the values unless the engine sets `UseRawPath` or
`UseEscapedPath` and clears `UnescapePathValues`. `Check` needs the adapter of
the `*gin.Engine`. Gin's `Any` registers one route for each method, so `Check` also
reports its `CONNECT` route.

## Fiber v3

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
app := fiber.New()
api := specout.Bind(doc, specoutfiber.New(app.Group("/v1")))
admin := specout.Bind(doc, specoutfiber.New(app.Group("/v1/admin", requireAuth)))

api.Get("/items/:id<int>", getItem)
admin.Post("/items", createItem) // the group middleware runs first

if err := specout.Check(doc, specoutfiber.New(app), "/openapi.json"); err != nil {
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

The handler reads parameters with `r.PathValue`. The values stay encoded
unless `fiber.Config.UnescapePath` is set. Fiber names wildcards `*1`,
`*2`, and `+1`. Fiber answers `HEAD` for every `GET` route. The document lists
the `GET` operation only, like http.ServeMux. `Check` needs the adapter of the
`*fiber.App`. It
ignores middleware routes and the `HEAD` routes that Fiber adds for `GET`
routes. It reports a `HEAD` route that is registered on its own. It also compares each translated path with the parameters that Fiber
parsed, so a path that specout reads differently fails `Check`.

Register routes on the app that serves them. A sub-app mounted with `app.Use`
changes the path after registration. Use a group instead.

## Path rules for the adapters

Literal `{` and `}` in an Echo, Gin, or Fiber route path panic at
registration, because OpenAPI reads braces as parameters. A path that the
adapter cannot translate also panics. Gin and Fiber keep no route for it. Echo
shows the group prefix only after it adds the route, so a bad Echo group prefix
panics after Echo added the route.

Every adapter checks the full path for empty segments (`//`) and dot segments
(`/.`, `/..`) after the router adds the route. Thus with chi and gorilla/mux,
the router has the route when the panic occurs.

A second registration of the same method and path panics, also on routers that
allow it: `echo.New()` lets a later route replace an earlier one, and Fiber
never runs the second handler.

## Other routers

An adapter is a small value that implements `specout.Router`. `Add` registers
the handler on the router and returns the full path in OpenAPI syntax. `Routes`
returns the route table for `Check`. Add copies the path parameters of the
router into `r.PathValue`, if the router does not set them:

```go
type myRouter struct{ r *otherrouter.Router }

func (m myRouter) Add(method, path string, h http.Handler) string {
	m.r.Handle(method, path, h)
	return strings.ReplaceAll(path, "<id>", "{id}") // the path in OpenAPI syntax
}

func (m myRouter) Routes() ([]specout.Route, error) {
	var out []specout.Route
	for _, rt := range m.r.Table() {
		out = append(out, specout.Route{Method: rt.Method, Path: rt.Path})
	}
	return out, nil
}

api := specout.Bind(doc, myRouter{r})
api.Get("/items/<id>", getItem)
```

`Document` adds metadata without changing runtime routing. Use an absolute
OpenAPI path and register the real handler separately:

```go
h := specout.Handler[getItemRequest, item]{
	HandlerFunc: getItem,
	Summary:     "Get an item",
}

router.Handle(http.MethodGet, "/items/{id}", h.HandlerFunc)
doc.Document(http.MethodGet, "/items/{id}", h)
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

`Build` returns the deterministic JSON, indented. Call it at startup, after
every route is registered, and serve the bytes on a route of your router:

```go
spec, err := doc.Build()
if err != nil {
	return err
}

// http.ServeMux, chi, gorilla/mux
mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(spec)
})

// Gin
engine.GET("/openapi.json", func(c *gin.Context) { c.Data(200, "application/json", spec) })

// Echo
e.GET("/openapi.json", func(c echo.Context) error { return c.Blob(200, "application/json", spec) })

// Fiber
app.Get("/openapi.json", func(c fiber.Ctx) error { return c.Type("json").Send(spec) })
```

The first successful `Build` freezes the document. Registering a route, schema
name, or union variant after that point panics. A failed build does not freeze
the document, and the next `Build` runs again.

## Route coverage

For chi, gorilla/mux, Echo, Gin, and Fiber, check the error of `Check` in a
test:

```go
func TestRoutesAreDocumented(t *testing.T) {
	doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
	router := chi.NewRouter()
	registerRoutes(specout.Bind(doc, specoutchi.New(router)))
	require.NoError(t, specout.Check(doc, specoutchi.New(router), "/openapi.json", "/healthz"))
}
```

A skip is an exact path or a trailing wildcard such as `/debug/*`. Skip only
endpoints that are intentionally outside the API, such as the spec endpoint.

## Status verification with recorder

Recorder is optional. Use it when existing HTTP tests can send requests through
a wrapped http.ServeMux, or through a router with the `Record` middleware of
its adapter.

```go
func TestObservedStatusesMatch(t *testing.T) {
	doc, mux := newAPI()
	recorded := recorder.New(mux, "/openapi.json")

	recorded.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/items", nil),
	)
	// Exercise every declared route and route-specific status through recorded.

	require.NoError(t, recorder.Verify(doc, recorded))
}
```

`recorder.New` takes the paths to skip as strings. `Verify` returns an error
that lists each problem on one line, or nil. `Verify` checks both directions:

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
Pass the spec endpoint to `recorder.New` as a skip.

Every relevant request must pass through the `Recorder`. Do not wrap only the
final assertion. The recorder reads the pattern of the http.ServeMux route that
ran. For another handler that you pass to `recorder.New`, the recorder finds
the route from the request path. You can pass the production handler, with its
middleware, to `recorder.New`. Middleware outside the router hides the route of
a request, so `Verify` finds the route from the request path, and a literal
segment wins over a parameter. A `401` from an outer auth middleware then counts for the route. A
`404` or `405` without a route is the router's answer, and counts for no route.
When no request matches a documented path, for example behind
`http.StripPrefix`, `Verify` reports that no route matched.

For chi, gorilla/mux, Echo, Gin, and Fiber, create the recorder without a
router and install the adapter's `Record` middleware:

```go
rec := recorder.New(nil, "/openapi.json")
engine := gin.New()
engine.Use(specoutgin.Record(rec)) // before any route
registerRoutes(specout.Bind(doc, specoutgin.New(engine)))

engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items", nil))
require.NoError(t, recorder.Verify(doc, rec))
```

| Router | Install | Middleware responses |
|---|---|---|
| chi | `r.Use(specoutchi.Record(rec))` first, before any route. | Recorded. `Record` finds the route that the request would reach. |
| gorilla/mux | `r.Use(specoutgorilla.Record(rec))`. gorilla runs it after it matches a route. | Recorded for a matched route. |
| Echo | `e.Use(specoutecho.Record(rec))` before other `e.Use` middleware. Route order does not matter. | Recorded. |
| Gin | `engine.Use(specoutgin.Record(rec))` before any route. | Recorded. |
| Fiber | `app.Use(specoutfiber.Record(rec))` before any route. | Recorded. `Record` finds the endpoint with `fiber.RoutePatternMatch`. |

Echo and Fiber pass a returned error to the framework's error handler, so the
recorder sees the status that the client gets. Fiber records a `HEAD` request
to a `GET` route under `GET`.

Recorder cannot recover matched route templates from routers that use
`Document` as their only integration. It finds their routes from the request
path.

## Minimum checks

Run the repository's existing commands. The normal minimum is:

```sh
go test ./...
```

Also run existing lint, OpenAPI validation, and client-generation checks when
the repository already has them. Add a golden OpenAPI file only when stable
spec review is a project requirement.
