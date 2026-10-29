// Package specoutechov4 binds specout handlers to Echo v4. Handlers stay plain
// net/http handlers: each route copies Echo's path parameters into
// r.PathValue before the handler runs, which echo.WrapHandler does not do in
// v4. The values are decoded, as on http.ServeMux. Adopt checks Echo's route
// table for routes that carry no specout metadata, and Record feeds the
// recorder status check.
package specoutechov4

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/echopath"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
	"github.com/labstack/echo/v4"
)

// Routes is what a Router registers through: *echo.Echo and *echo.Group.
type Routes interface {
	Add(method, path string, handler echo.HandlerFunc, middleware ...echo.MiddlewareFunc) *echo.Route
}

// Router registers specout handlers on an Echo instance or group. Each route
// is documented at the full path Echo reports for it, group prefix included.
type Router struct {
	d *specout.Generator
	r Routes
}

// New returns the Echo binder for d on r.
func New(d *specout.Generator, r Routes) *Router {
	if d == nil || r == nil {
		panic("specoutechov4: New needs a generator and an *echo.Echo or *echo.Group")
	}
	return &Router{d: d, r: r}
}

// Handle registers h for method on path. Route middleware m runs before h.
func (b *Router) Handle[Req, Res any](method, path string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	// Reject the route's own syntax before Echo serves it. The full-path check
	// below can only panic after the route is live.
	if _, _, err := echopath.OpenAPI(path); err != nil {
		panic(fmt.Sprintf("specoutechov4: %s %s: %v", method, path, err))
	}
	// Echo shows the group prefix only in the Route it returns, and a route
	// replaces an earlier one on its path. So the route answers 404 until Bind
	// returns: a registration that panics serves nothing, even when the caller
	// recovers.
	var (
		bound atomic.Bool
		route *echo.Route
	)
	specout.Bind(b.d, method, path, h, func(next http.Handler) string {
		route = b.r.Add(method, path, func(c echo.Context) error {
			if !bound.Load() {
				return echo.ErrNotFound
			}
			return serve(c, next)
		}, m...)
		// Echo v4 lists no parameters in its route table, so the translation
		// has no router-side check here. Its scanner is the one echopath
		// follows.
		full, _, err := echopath.OpenAPI(route.Path)
		if err != nil {
			panic(fmt.Sprintf("specoutechov4: %s %s: %v", method, route.Path, err))
		}
		return full
	})
	registered.Store(route, struct{}{})
	bound.Store(true)
}

// registered holds each Echo route that a Router registered, for Adopt. Echo
// lists the same *echo.Route in its route table until a later route on the
// path replaces it.
var registered sync.Map

// serve runs next for one request. Echo v4 keeps path parameters on its own
// context, so they are copied onto the request first, as Echo v5's
// WrapHandler does. Echo v4 routes on r.URL.RawPath when the request has one,
// and then its values are still encoded: they are decoded here, as
// http.ServeMux reports them.
func serve(c echo.Context, next http.Handler) error {
	r := c.Request()
	r.Pattern = c.Path()
	values := c.ParamValues()
	encoded := r.URL.RawPath != ""
	for i, name := range c.ParamNames() {
		if i < len(values) {
			r.SetPathValue(name, pathValue(values[i], encoded))
		}
	}
	next.ServeHTTP(c.Response(), r)
	return nil
}

// pathValue returns v decoded when Echo read it from the encoded path. A value
// that does not decode stays as Echo gave it.
func pathValue(v string, encoded bool) string {
	if !encoded {
		return v
	}
	if decoded, err := url.PathUnescape(v); err == nil {
		return decoded
	}
	return v
}

// Get registers a GET route on p.
func (b *Router) Get[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodGet, p, h, m...)
}

// Head registers a HEAD route on p.
func (b *Router) Head[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodHead, p, h, m...)
}

// Post registers a POST route on p.
func (b *Router) Post[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodPost, p, h, m...)
}

// Put registers a PUT route on p.
func (b *Router) Put[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodPut, p, h, m...)
}

// Patch registers a PATCH route on p.
func (b *Router) Patch[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodPatch, p, h, m...)
}

// Delete registers a DELETE route on p.
func (b *Router) Delete[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodDelete, p, h, m...)
}

// Options registers an OPTIONS route on p.
func (b *Router) Options[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodOptions, p, h, m...)
}

// Trace registers a TRACE route on p.
func (b *Router) Trace[Req, Res any](p string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	b.Handle(http.MethodTrace, p, h, m...)
}

// anyMethods is the method list Echo v4's Any registers, one route each.
var anyMethods = []string{
	http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions,
	http.MethodPatch, http.MethodPost, echo.PROPFIND, http.MethodPut, http.MethodTrace, echo.REPORT,
}

// Adopt reports the routes of e and its host routers that specout cannot
// document: routes no Router registered, and Any routes, which serve every
// method. Echo v4 lists an Any route once per method; Adopt reports it once.
// The 404 routes that Echo adds for group middleware are not endpoints and are
// skipped. Skip the spec endpoint itself, as in specout.Skip("/openapi.json").
//
// A route documents its own router only. A host route that no Router
// registered, on the method and path of a route that a Router registered on
// another router, is reported with its host, as GET api.example.com/items.
func Adopt(d *specout.Generator, e *echo.Echo, skips ...specout.SkipRule) error {
	hosts := e.Routers()
	own := ownKeys(e.Router().Routes())
	for _, host := range hosts {
		maps.Copy(own, ownKeys(host.Routes()))
	}
	routes := routeKeys(e.Router().Routes())
	for name, host := range hosts {
		routes = append(routes, routeKeys(host.Routes())...)
		routes = append(routes, twins(name, host.Routes(), own, skips)...)
	}
	// Echo keeps its route tables in maps, so sort for a stable report.
	slices.SortFunc(routes, func(a, b specout.RouteKey) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return d.CheckRoutes(routes, skips...)
}

// routeKeys turns one router's table into route keys. A path whose Any
// methods all run one handler becomes a single key with no method.
func routeKeys(table []*echo.Route) []specout.RouteKey {
	byPath := make(map[string]map[string]string) // path -> method -> handler name
	for _, ri := range table {
		if ri.Method == echo.RouteNotFound {
			continue
		}
		if byPath[ri.Path] == nil {
			byPath[ri.Path] = make(map[string]string)
		}
		byPath[ri.Path][ri.Method] = ri.Name
	}
	var keys []specout.RouteKey
	for p, methods := range byPath {
		name := methods[http.MethodGet]
		if name != "" && !slices.ContainsFunc(anyMethods, func(m string) bool { return methods[m] != name }) {
			keys = append(keys, specout.RouteKey{Path: echopath.Table(p)})
			continue
		}
		for method := range methods {
			keys = append(keys, specout.RouteKey{Method: method, Path: echopath.Table(p)})
		}
	}
	return keys
}

// ownKeys returns the key of each route of table that a Router registered.
func ownKeys(table []*echo.Route) map[specout.RouteKey]bool {
	own := make(map[specout.RouteKey]bool)
	for _, ri := range table {
		if _, ok := registered.Load(ri); ok {
			own[specout.RouteKey{Method: ri.Method, Path: echopath.Table(ri.Path)}] = true
		}
	}
	return own
}

// twins returns a key, with host before the path, for each route of host's
// table that no Router registered and that has the key of a route in own. A
// route on host cannot share its key with another route of host, so that
// route is on another router. No document path starts with a host, so
// CheckRoutes reports the key. A route that skips cover stays out.
func twins(
	host string, table []*echo.Route, own map[specout.RouteKey]bool, skips []specout.SkipRule,
) []specout.RouteKey {
	var keys []specout.RouteKey
	for _, ri := range table {
		key := specout.RouteKey{Method: ri.Method, Path: echopath.Table(ri.Path)}
		if _, ok := registered.Load(ri); ok || !own[key] {
			continue
		}
		if !slices.ContainsFunc(skips, func(s specout.SkipRule) bool { return s.Matches(key.Path) }) {
			keys = append(keys, specout.RouteKey{Method: key.Method, Path: host + key.Path})
		}
	}
	return keys
}

// RequireDocumented fails the test when e holds routes specout cannot
// document.
func RequireDocumented(t specout.TestingT, d *specout.Generator, e *echo.Echo, skips ...specout.SkipRule) {
	t.Helper()
	if err := Adopt(d, e, skips...); err != nil {
		t.Errorf("%v", err)
	}
}

// Record returns Echo middleware that reports the status of every matched
// route to rec, for recorder.Verify. Register it first with e.Use. Echo routes
// before it runs middleware, so a response that other middleware writes, such
// as a 401, still counts for its route. Record hands a returned error to the
// Echo error handler, so it reads the status the client gets.
//
// A handler that hijacks the connection writes the response itself: a
// WebSocket handshake that wrote no status counts as 101, and any other
// hijack without a status counts as nothing.
func Record(rec *recorder.Recorder) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			resp := c.Response()
			writer := resp.Writer
			w, watch := hijack.Wrap(writer)
			resp.Writer = w
			if err := next(c); err != nil {
				c.Error(err)
			}
			resp.Writer = writer
			status, ok := hijack.Status(c.Request().Header, resp.Status, resp.Committed, watch.Hijacked())
			if ok && routed(c) {
				rec.Observe(c.Request().Method, echopath.Table(c.Path()), status)
			}
			return nil
		}
	}
}

// routed reports whether the request reached a route for its own method.
// Echo also sets the context path for a 405, for its automatic OPTIONS answer
// and for a group 404 route, so the route table decides.
func routed(c echo.Context) bool {
	e, r := c.Echo(), c.Request()
	router := e.Router()
	if host, ok := e.Routers()[r.Host]; ok {
		router = host
	}
	return slices.ContainsFunc(router.Routes(), func(ri *echo.Route) bool {
		return ri.Method == r.Method && ri.Path == c.Path()
	})
}
