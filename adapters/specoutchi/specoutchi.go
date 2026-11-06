// Package specoutchi is the specout adapter for chi. Handlers stay plain
// net/http handlers, and chi sets r.PathValue. Record feeds the recorder
// status check.
package specoutchi

import (
	"cmp"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
)

// Router is the adapter of a chi.Router, for specout.Bind. A chi router does
// not know the prefix where it is routed or mounted, so pass it to New: inside
// r.Route("/v1", ...), use New(r, "/v1"). Put route middleware on the router:
// New(r.With(auth)).
type Router struct {
	r      chi.Router
	prefix string
}

// New returns the adapter of r. prefix is the path where r is routed or
// mounted, if any.
func New(r chi.Router, prefix ...string) *Router {
	if nilRouter(r) || len(prefix) > 1 {
		panic("specoutchi: New needs a chi.Router and at most one prefix")
	}
	return &Router{r: r, prefix: strings.Join(prefix, "")}
}

// nilRouter reports a nil router, also a nil *chi.Mux in the interface, which
// r == nil does not catch.
func nilRouter(r chi.Router) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// Add registers h for method on p, in chi syntax, and returns its full path in
// OpenAPI syntax.
func (b *Router) Add(method, p string, h http.Handler) string {
	b.r.Method(method, p, h)
	return openAPIPath(join(b.prefix, p))
}

// Routes returns the route table of the router, with the prefix in front of
// each path. chi lists a route that serves every method once for each method.
func (b *Router) Routes() ([]specout.Route, error) {
	var routes []specout.Route
	err := chi.Walk(b.r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, specout.Route{Method: method, Path: openAPIPath(join(b.prefix, walkPath(route)))})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("specoutchi: walk chi router: %w", err)
	}
	return routes, nil
}

// join adds a prefix to a pattern, as chi joins a mount pattern and the
// routes of its subrouter.
func join(prefix, p string) string {
	if prefix == "" {
		return p
	}
	return strings.TrimSuffix(prefix, "/") + p
}

// openAPIPath translates a chi path: a trailing * becomes the catch-all
// {*...}. chi parameters, {id} and {id:[0-9]+}, are OpenAPI syntax already.
func openAPIPath(p string) string {
	if rest, ok := strings.CutSuffix(p, "*"); ok {
		return rest + "{*...}"
	}
	return p
}

// walkPath removes the /* that chi.Walk leaves where a router is mounted.
// chi.Walk replaces each /*/ once, so a mount at / inside a group leaves
// /v1/*/x behind. chi allows * only at the end of a pattern, so a /*/ is
// always a mount.
func walkPath(route string) string {
	for strings.Contains(route, "/*/") {
		route = strings.ReplaceAll(route, "/*/", "/")
	}
	return route
}

// Record returns chi middleware that reports the status of every matched
// route to rec, for recorder.Verify. Register it first with r.Use. chi
// routes after its middleware runs, so when other middleware writes the
// response, such as a 401, Record finds the route that the request would
// have reached.
//
// A handler that hijacks the connection writes the response itself: a
// WebSocket handshake that wrote no status counts as 101, and any other
// hijack without a status counts as nothing.
func Record(rec *recorder.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rctx := chi.RouteContext(r.Context())
			if rctx == nil || rctx.Routes == nil {
				next.ServeHTTP(w, r)
				return
			}
			// Find matches the escaped path, as chi does.
			fallback := find(rctx.Routes, r.Method, r.URL.EscapedPath())
			hw, watch := hijack.Wrap(w)
			next.ServeHTTP(hw, r)
			method, pattern := ranRoute(rctx, r.Method, fallback)
			if status, ok := watch.Result(r.Header); ok && pattern != "" {
				bridge.Observe(rec, method, openAPIPath(pattern), status)
			}
		})
	}
}

// ranRoute returns the method and pattern of the route that ran, or method
// and fallback when middleware answered before chi routed the request.
func ranRoute(rctx *chi.Context, method, fallback string) (string, string) {
	if len(rctx.RoutePatterns) == 0 {
		return method, fallback
	}
	return cmp.Or(rctx.RouteMethod, method), cmp.Or(routePattern(rctx), fallback)
}

// routePattern returns the pattern of the endpoint that ran. A pattern stack
// that ends at a subrouter is an unmatched request or a middleware response,
// not an endpoint. chi.Context.RoutePattern trims trailing slashes, so the
// stack is joined here.
func routePattern(ctx *chi.Context) string {
	routes := ctx.Routes
	patterns := slices.Clone(ctx.RoutePatterns)
	for i, pattern := range patterns {
		if routes == nil {
			return ""
		}
		var next chi.Routes
		found := false
		for _, route := range routes.Routes() {
			if route.Pattern == pattern {
				next, found = route.SubRoutes, true
				break
			}
		}
		// A request for /users itself runs through a mount stub that Routes
		// does not list; the subrouter behind /users/* serves it.
		if mount, ok := mountOf(routes, pattern); !found && ok {
			patterns[i], next = mount.Pattern, mount.SubRoutes
		}
		routes = next
	}
	if routes != nil {
		return ""
	}
	return walkPath(strings.Join(patterns, ""))
}

// mountOf returns the mount that a chi stub pattern belongs to. Mounting at
// /users registers /users, /users/ and /users/*, and lists only /users/*.
func mountOf(routes chi.Routes, pattern string) (chi.Route, bool) {
	if pattern == "" || strings.HasSuffix(pattern, "*") {
		return chi.Route{}, false
	}
	want := strings.TrimSuffix(pattern, "/") + "/*"
	for _, route := range routes.Routes() {
		if route.Pattern == want && route.SubRoutes != nil {
			return route, true
		}
	}
	return chi.Route{}, false
}

// find is routes.Find with mount stubs resolved. Find stops at the stub for
// /users, so the route that serves it is the subrouter's own / route, and a
// subrouter without one leaves the request unmatched.
func find(routes chi.Routes, method, path string) string {
	pattern := routes.Find(chi.NewRouteContext(), method, path)
	mount, ok := mountOf(routes, pattern)
	if !ok {
		return pattern
	}
	sub := find(mount.SubRoutes, method, "/")
	if sub == "" {
		return ""
	}
	return walkPath(mount.Pattern + sub)
}
