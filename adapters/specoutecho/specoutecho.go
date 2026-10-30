// Package specoutecho binds specout handlers to Echo v5. Handlers stay plain
// net/http handlers: echo.WrapHandler copies Echo's path parameters into
// r.PathValue. Adopt checks Echo's route table for routes that carry no
// specout metadata, and Record feeds the recorder status check.
//
// Echo v5 leaves a path value encoded when the request path is encoded:
// /users/a%40b gives a%40b. To get a@b, as on http.ServeMux, set
// echo.RouterConfig.UnescapePathParamValues. specout does not decode the
// values itself: it cannot read that setting, and would decode them twice.
package specoutecho

import (
	"fmt"
	"net/http"
	"slices"
	"sync/atomic"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/echopath"
	"github.com/happytoolin/specout/internal/hijack"
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
	if _, _, err := echopath.OpenAPI(path); err != nil {
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
		full, names, err := echopath.OpenAPI(ri.Path)
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
		routes = append(routes, specout.RouteKey{Method: method, Path: echopath.Table(ri.Path)})
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
//
// A handler that hijacks the connection writes the response itself: a
// WebSocket handshake that wrote no status counts as 101, and any other
// hijack without a status counts as nothing.
func Record(rec *recorder.Recorder) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			watch, undo := watchHijack(c)
			if err := next(c); err != nil {
				c.Echo().HTTPErrorHandler(c, err)
			}
			undo()
			ri := c.RouteInfo()
			method := ri.Method
			switch method {
			case "", echo.RouteNotFound:
				return nil
			case echo.RouteAny:
				method = c.Request().Method
			}
			resp, code := echo.ResolveResponseStatus(c.Response(), nil)
			if status, ok := hijack.Status(c.Request().Header, code, resp != nil && resp.Committed, watch.Hijacked()); ok {
				rec.Observe(method, echopath.Table(ri.Path), status)
			}
			return nil
		}
	}
}

// watchHijack makes the writer under the echo.Response of c note a Hijack.
// undo puts the writer back.
func watchHijack(c *echo.Context) (*hijack.Watch, func()) {
	resp, err := echo.UnwrapResponse(c.Response())
	if err != nil {
		return nil, func() {}
	}
	writer := resp.ResponseWriter
	w, watch := hijack.Wrap(writer)
	resp.ResponseWriter = w
	return watch, func() { resp.ResponseWriter = writer }
}
