// Package specoutechov4 is the specout adapter for Echo v4. Handlers stay
// plain net/http handlers: each route copies Echo's path parameters into
// r.PathValue before the handler runs, which echo.WrapHandler does not do in
// v4. Record feeds the recorder status check.
package specoutechov4

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
	"github.com/labstack/echo/v4"
)

// Routes is what a Router registers through: *echo.Echo and *echo.Group.
type Routes interface {
	Add(method, path string, handler echo.HandlerFunc, middleware ...echo.MiddlewareFunc) *echo.Route
}

// Router is the adapter of an Echo instance or group, for specout.Bind. Each
// route is documented at the full path Echo reports for it, group prefix
// included. Put route middleware on a group: e.Group("/admin", auth).
type Router struct{ r Routes }

// New returns the adapter of r.
func New(r Routes) *Router {
	if r == nil {
		panic("specoutechov4: New needs an *echo.Echo or *echo.Group")
	}
	return &Router{r: r}
}

// Add registers h for method on path, in Echo syntax, and returns its full
// path in OpenAPI syntax.
func (b *Router) Add(method, path string, h http.Handler) string {
	// Reject the route's own syntax before Echo serves it. The full-path check
	// below can only panic after the route is live.
	if _, _, err := echopath.OpenAPI(path); err != nil {
		panic(fmt.Sprintf("specoutechov4: %s %s: %v", method, path, err))
	}
	route := b.r.Add(method, path, func(c echo.Context) error { return serve(c, h) })
	// Echo v4 lists no parameters in its route table, so the translation has
	// no router-side check here. Its scanner is the one echopath follows.
	full, _, err := echopath.OpenAPI(route.Path)
	if err != nil {
		panic(fmt.Sprintf("specoutechov4: %s %s: %v", method, route.Path, err))
	}
	return full
}

// serve runs next for one request. Echo v4 keeps path parameters on its own
// context, so they are copied onto the request first, as Echo gives them.
func serve(c echo.Context, next http.Handler) error {
	r := c.Request()
	values := c.ParamValues()
	for i, name := range c.ParamNames() {
		if i < len(values) {
			r.SetPathValue(name, values[i])
		}
	}
	next.ServeHTTP(c.Response(), r)
	return nil
}

// Routes returns the route tables of the Echo instance and of its host
// routers. Echo v4 lists an Any route once per method. The 404 routes that
// Echo adds for group middleware are not endpoints and are left out. A group
// lists no routes, so pass Check the adapter of the instance.
func (b *Router) Routes() ([]specout.Route, error) {
	e, ok := b.r.(*echo.Echo)
	if !ok {
		return nil, errors.New("specoutechov4: a group lists no routes; pass Check specoutechov4.New(e)")
	}
	routes := tableRoutes(e.Router().Routes())
	for _, host := range e.Routers() {
		routes = append(routes, tableRoutes(host.Routes())...)
	}
	return routes, nil
}

func tableRoutes(table []*echo.Route) []specout.Route {
	var routes []specout.Route
	for _, ri := range table {
		if ri.Method != echo.RouteNotFound {
			routes = append(routes, specout.Route{Method: ri.Method, Path: echopath.Table(ri.Path)})
		}
	}
	return routes
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
			defer func() { resp.Writer = writer }()
			if err := next(c); err != nil {
				c.Error(err)
			}
			status, ok := hijack.Status(c.Request().Header, resp.Status, resp.Committed, watch.Hijacked())
			if ok && routed(c) {
				bridge.Observe(rec, c.Request().Method, echopath.Table(c.Path()), status)
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
