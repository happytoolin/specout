// Package specoutecho is the specout adapter for Echo v5. Handlers stay plain
// net/http handlers: echo.WrapHandler copies Echo's path parameters into
// r.PathValue. Record feeds the recorder status check.
//
// Echo v5 leaves a path value encoded when the request path is encoded:
// /users/a%40b gives a%40b. To get a@b, as on http.ServeMux, set
// echo.RouterConfig.UnescapePathParamValues. specout does not decode the
// values itself: it cannot read that setting, and would decode them twice.
package specoutecho

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/internal/echopath"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
	"github.com/labstack/echo/v5"
)

// Routes is what a Router registers through: *echo.Echo and *echo.Group.
type Routes interface {
	AddRoute(route echo.Route) (echo.RouteInfo, error)
}

// Router is the adapter of an Echo instance or group, for specout.Bind. Each
// route is documented at the full path Echo reports for it, group prefix
// included. Put route middleware on a group: e.Group("/admin", auth).
type Router struct{ r Routes }

// New returns the adapter of r.
func New(r Routes) *Router {
	if r == nil {
		panic("specoutecho: New needs an *echo.Echo or *echo.Group")
	}
	return &Router{r: r}
}

// Add registers h for method on path, in Echo syntax, and returns its full
// path in OpenAPI syntax.
func (b *Router) Add(method, path string, h http.Handler) string {
	// Reject the route's own syntax before Echo serves it. The full-path check
	// below can only panic after the route is live.
	if _, _, err := echopath.OpenAPI(path); err != nil {
		panic(fmt.Sprintf("specoutecho: %s %s: %v", method, path, err))
	}
	ri, err := b.r.AddRoute(echo.Route{Method: method, Path: path, Handler: echo.WrapHandler(h)})
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
}

// Routes returns the route table of the Echo instance. An Any route serves
// every method. The 404 routes that Echo adds for group middleware are not
// endpoints and are left out. A group lists no routes, so pass Check the
// adapter of the instance.
func (b *Router) Routes() ([]specout.Route, error) {
	e, ok := b.r.(*echo.Echo)
	if !ok {
		return nil, errors.New("specoutecho: a group lists no routes; pass Check specoutecho.New(e)")
	}
	table := e.Router().Routes()
	routes := make([]specout.Route, 0, len(table))
	for _, ri := range table {
		method := ri.Method
		switch method {
		case echo.RouteNotFound:
			continue
		case echo.RouteAny:
			method = ""
		}
		routes = append(routes, specout.Route{Method: method, Path: echopath.Table(ri.Path)})
	}
	return routes, nil
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
			defer undo()
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
			resp, code := echo.ResolveResponseStatus(c.Response(), nil)
			if status, ok := hijack.Status(c.Request().Header, code, resp != nil && resp.Committed, watch.Hijacked()); ok {
				bridge.Observe(rec, method, echopath.Table(ri.Path), status)
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
