// Package specoutechov4 binds specout handlers to Echo v4. Handlers stay plain
// net/http handlers: each route copies Echo's path parameters into
// r.PathValue before the handler runs, which echo.WrapHandler does not do in
// v4. Adopt checks Echo's route table for routes that carry no specout
// metadata, and Record feeds the recorder status check.
package specoutechov4

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/happytoolin/specout"
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
	if _, err := openAPIPath(path); err != nil {
		panic(fmt.Sprintf("specoutechov4: %s %s: %v", method, path, err))
	}
	// Echo shows the group prefix only in the Route it returns, and a route
	// replaces an earlier one on its path. So the route answers 404 until Bind
	// returns: a registration that panics serves nothing, even when the caller
	// recovers.
	var bound atomic.Bool
	specout.Bind(b.d, method, path, h, func(next http.Handler) string {
		route := b.r.Add(method, path, func(c echo.Context) error {
			if !bound.Load() {
				return echo.ErrNotFound
			}
			return serve(c, next)
		}, m...)
		// Echo v4 lists no parameters in its route table, so the translation
		// has no router-side check here. Its scanner is the one openAPIPath
		// follows.
		full, err := openAPIPath(route.Path)
		if err != nil {
			panic(fmt.Sprintf("specoutechov4: %s %s: %v", method, route.Path, err))
		}
		return full
	})
	bound.Store(true)
}

// serve runs next for one request. Echo v4 keeps path parameters on its own
// context, so they are copied onto the request first, as Echo v5's
// WrapHandler does.
func serve(c echo.Context, next http.Handler) error {
	r := c.Request()
	r.Pattern = c.Path()
	values := c.ParamValues()
	for i, name := range c.ParamNames() {
		if i < len(values) {
			r.SetPathValue(name, values[i])
		}
	}
	next.ServeHTTP(c.Response(), r)
	return nil
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
func Adopt(d *specout.Generator, e *echo.Echo, skips ...specout.SkipRule) error {
	routes := routeKeys(e.Router().Routes())
	for _, host := range e.Routers() {
		routes = append(routes, routeKeys(host.Routes())...)
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
			keys = append(keys, specout.RouteKey{Path: tablePath(p)})
			continue
		}
		for method := range methods {
			keys = append(keys, specout.RouteKey{Method: method, Path: tablePath(p)})
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
func Record(rec *recorder.Recorder) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if err := next(c); err != nil {
				c.Error(err)
			}
			if routed(c) {
				rec.Observe(c.Request().Method, tablePath(c.Path()), c.Response().Status)
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

// tablePath translates a path from Echo's route table. A path Handle could
// not have registered stays as Echo spells it, so a report still names it.
func tablePath(p string) string {
	full, err := openAPIPath(p)
	if err != nil {
		return p
	}
	return full
}

// openAPIPath translates an Echo route path into OpenAPI syntax: /users/:id
// becomes /users/{id}, and an escaped \: is a literal colon, so /:id\:cancel
// becomes /{id}:cancel. A * matches the rest of the path, so it becomes the
// catch-all {*...} and Echo ignores anything after it. The scan follows Echo's
// walkRoutePath, which also names the parameters Echo sets.
func openAPIPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Echo's router adds the leading slash the same way
	}
	var b strings.Builder
	for i := 0; i < len(p); {
		switch {
		case isEscapedColon(p, i):
			b.WriteByte(':')
			i += 2
		case p[i] == ':':
			start := i + 1
			i = paramEnd(p, start)
			name := p[start:i]
			if name == "" || strings.ContainsAny(name, `{}:*\`) {
				return "", fmt.Errorf("parameter name %q cannot be an OpenAPI parameter", name)
			}
			b.WriteString("{" + name + "}")
		case p[i] == '*':
			b.WriteString("{*...}")
			return b.String(), nil
		default:
			start := i
			for i < len(p) && p[i] != ':' && p[i] != '*' && !isEscapedColon(p, i) {
				i++
			}
			static := p[start:i]
			if strings.ContainsAny(static, "{}") {
				return "", fmt.Errorf("literal braces in %q read as OpenAPI parameters", static)
			}
			b.WriteString(static)
		}
	}
	return b.String(), nil
}

// paramEnd returns the end of the parameter name that starts at start. The
// name runs to the next slash, unless an escaped colon starts an inline verb
// such as \:cancel.
func paramEnd(p string, start int) int {
	plain := true
	i := start
	for i < len(p) && p[i] != '/' {
		if isEscapedColon(p, i) {
			if plain && isInlineVerb(p[i+2:]) {
				return i
			}
			for i < len(p) && p[i] != '/' {
				i++
			}
			return i
		}
		if p[i] == ':' || p[i] == '*' {
			plain = false
		}
		i++
	}
	return i
}

func isEscapedColon(p string, i int) bool {
	return p[i] == '\\' && i+1 < len(p) && p[i+1] == ':'
}

// isInlineVerb reports whether the text after an escaped colon stays static up
// to the end of its segment.
func isInlineVerb(rest string) bool {
	for i := 0; i < len(rest) && rest[i] != '/'; i++ {
		switch rest[i] {
		case '*':
			return false
		case ':':
			if i == 0 || rest[i-1] != '\\' {
				return false
			}
		}
	}
	return true
}
