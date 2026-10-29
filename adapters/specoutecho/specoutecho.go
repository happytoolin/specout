// Package specoutecho binds specout handlers to Echo v5. Handlers stay plain
// net/http handlers: echo.WrapHandler copies Echo's path parameters into
// r.PathValue. Adopt checks Echo's route table for routes that carry no
// specout metadata, and Record feeds the recorder status check.
package specoutecho

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
	"github.com/labstack/echo/v5"
)

// Routes is what a Router registers through: *echo.Echo and *echo.Group.
type Routes interface {
	AddRoute(route echo.Route) (echo.RouteInfo, error)
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
		panic("specoutecho: New needs a generator and an *echo.Echo or *echo.Group")
	}
	return &Router{d: d, r: r}
}

// Handle registers h for method on path. Route middleware m runs before h.
func (b *Router) Handle[Req, Res any](method, path string, h specout.Handler[Req, Res], m ...echo.MiddlewareFunc) {
	// Reject the route's own syntax before Echo serves it. The full-path check
	// below can only panic after the route is live.
	if _, _, err := openAPIPath(path); err != nil {
		panic(fmt.Sprintf("specoutecho: %s %s: %v", method, path, err))
	}
	// Echo shows the group prefix only in the RouteInfo of an added route, and
	// a route replaces an earlier one on its path. So the route answers 404
	// until Bind returns: a registration that panics serves nothing, even when
	// the caller recovers.
	var bound atomic.Bool
	specout.Bind(b.d, method, path, h, func(next http.Handler) string {
		serve := echo.WrapHandler(next)
		ri, err := b.r.AddRoute(echo.Route{
			Method: method,
			Path:   path,
			Handler: func(c *echo.Context) error {
				if !bound.Load() {
					return echo.ErrNotFound
				}
				return serve(c)
			},
			Middlewares: m,
		})
		if err != nil {
			panic(fmt.Sprintf("specoutecho: %s %s: %v", method, path, err))
		}
		full, names, err := openAPIPath(ri.Path)
		// The router's own parameter list is the check on the translation: a
		// path Echo reads differently would document parameters it never sets.
		if err == nil && !slices.Equal(names, ri.Parameters) {
			err = fmt.Errorf("echo reads parameters %q, specout read %q", ri.Parameters, names)
		}
		if err != nil {
			panic(fmt.Sprintf("specoutecho: %s %s: %v", method, ri.Path, err))
		}
		return full
	})
	bound.Store(true)
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

// Adopt reports the routes of e that specout cannot document: routes no
// Router registered, and Any routes, which serve every method. The 404 routes
// that Echo adds for group middleware are not endpoints and are skipped. Skip
// the spec endpoint itself, as in specout.Skip("/openapi.json").
func Adopt(d *specout.Generator, e *echo.Echo, skips ...specout.SkipRule) error {
	table := e.Router().Routes()
	routes := make([]specout.RouteKey, 0, len(table))
	for _, ri := range table {
		method := ri.Method
		switch method {
		case echo.RouteNotFound:
			continue
		case echo.RouteAny:
			method = ""
		}
		routes = append(routes, specout.RouteKey{Method: method, Path: tablePath(ri.Path)})
	}
	return d.CheckRoutes(routes, skips...)
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
		return func(c *echo.Context) error {
			if err := next(c); err != nil {
				c.Echo().HTTPErrorHandler(c, err)
			}
			ri := c.RouteInfo()
			method := ri.Method
			switch method {
			case "", echo.RouteNotFound:
				return nil
			case echo.RouteAny:
				method = c.Request().Method
			}
			_, status := echo.ResolveResponseStatus(c.Response(), nil)
			rec.Observe(method, tablePath(ri.Path), status)
			return nil
		}
	}
}

// tablePath translates a path from Echo's route table. A path Handle could
// not have registered stays as Echo spells it, so a report still names it.
func tablePath(p string) string {
	full, _, err := openAPIPath(p)
	if err != nil {
		return p
	}
	return full
}

// openAPIPath translates an Echo route path into OpenAPI syntax and lists the
// parameter names in Echo's order: /users/:id becomes /users/{id}, and an
// escaped \: is a literal colon, so /:id\:cancel becomes /{id}:cancel. A *
// matches the rest of the path, so it becomes the catch-all {*...} and Echo
// ignores anything after it. The scan follows Echo's walkRoutePath.
func openAPIPath(p string) (string, []string, error) {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Echo's router adds the leading slash the same way
	}
	var b strings.Builder
	var names []string
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
				return "", nil, fmt.Errorf("parameter name %q cannot be an OpenAPI parameter", name)
			}
			names = append(names, name)
			b.WriteString("{" + name + "}")
		case p[i] == '*':
			names = append(names, "*")
			b.WriteString("{*...}")
			return b.String(), names, nil
		default:
			start := i
			for i < len(p) && p[i] != ':' && p[i] != '*' && !isEscapedColon(p, i) {
				i++
			}
			static := p[start:i]
			if strings.ContainsAny(static, "{}") {
				return "", nil, fmt.Errorf("literal braces in %q read as OpenAPI parameters", static)
			}
			b.WriteString(static)
		}
	}
	return b.String(), names, nil
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
