// Package recorder wraps a router and reports the status codes the router
// actually returned per route, so a test can compare live behaviour against
// the documented spec.
package recorder

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	pathpkg "path"
	"slices"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
)

// Recorder wraps a router and observes status codes per route, keyed by the
// matched pattern. Method-less matches (404s) record nothing.
type Recorder struct {
	next  http.Handler
	skips []specout.SkipRule // routes that are not API drift, e.g. the spec endpoint

	mu    sync.Mutex
	codes map[specout.RouteKey]map[int]bool
	// unmatched counts requests ServeHTTP served without finding a route,
	// so Verify can tell a wrapped router from untested routes.
	unmatched int
}

// New wraps next (the app router) for observation: a route matching a skip is
// served but not recorded. Pattern capture covers chi, gorilla/mux and
// net/http.ServeMux. Echo, Gin and Fiber report through router middleware
// instead (specoutecho.Record, specoutgin.Record, specoutfiber.Record): pass
// a nil next for them. A router specout reaches only through Document exposes
// no pattern, so Verify reports its routes as never produced.
func New(next http.Handler, skips ...specout.SkipRule) *Recorder {
	return &Recorder{next: next, skips: skips, codes: map[specout.RouteKey]map[int]bool{}}
}

// Observe records status for one matched route. path uses OpenAPI syntax, as
// the document spells it. Router middleware that knows the matched route calls
// it; ServeHTTP calls it for the routers it wraps. A path that matches a skip
// rule records nothing, and status 0 counts as 200.
func (rec *Recorder) Observe(method, path string, status int) {
	if path == "" || slices.ContainsFunc(rec.skips, func(s specout.SkipRule) bool { return s.Matches(path) }) {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	key := specout.NewRouteKey(method, path)
	if rec.codes[key] == nil {
		rec.codes[key] = map[int]bool{}
	}
	rec.codes[key][cmp.Or(status, http.StatusOK)] = true
}

func (rec *Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rec.next == nil {
		panic("recorder: ServeHTTP on a Recorder without a router; route requests through the router, " +
			"whose middleware reports to Observe")
	}
	pattern := rec.match(r)
	rctx := chi.RouteContext(r.Context())
	if routes, ok := rec.next.(chi.Routes); ok && rctx == nil {
		// Own the context so chi does not return it to its pool before we
		// read the final method and pattern chosen by routing middleware.
		rctx = chi.NewRouteContext()
		rctx.Routes = routes
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}
	rw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	rec.next.ServeHTTP(rw, r)

	// std populates r.Pattern during dispatch, so read it only after.
	method := r.Method
	if rctx != nil && len(rctx.RoutePatterns) > 0 {
		if actual := chiPattern(rctx); actual != "" {
			pattern = actual
		}
		method = cmp.Or(rctx.RouteMethod, method)
	}
	if pattern == "" && rctx == nil {
		pattern = stdPattern(r.Pattern)
		// ServeMux dispatches HEAD to GET only when no explicit HEAD route
		// matched. Record the handler that actually ran.
		if method == http.MethodHead && strings.HasPrefix(r.Pattern, http.MethodGet+" ") {
			method = http.MethodGet
		}
	}
	// a redirect the router made ran no route
	if routerRedirect(r, rw) {
		return
	}
	// unmatched, or a skipped route: no key, no drift entry
	if pattern == "" {
		rec.mu.Lock()
		rec.unmatched++
		rec.mu.Unlock()
		return
	}
	rec.Observe(method, pattern, rw.Status())
}

// routerRedirect reports a redirect that a router writes in place of the route:
// http.ServeMux and gorilla/mux redirect to the cleaned path, and ServeMux and
// gorilla StrictSlash add or drop a trailing slash. A route that redirects to
// its own path with the slash toggled reads the same, and is not recorded.
func routerRedirect(r *http.Request, rw middleware.WrapResponseWriter) bool {
	switch rw.Status() {
	case http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return false
	}
	loc, err := url.Parse(rw.Header().Get("Location"))
	if err != nil || loc.Path == "" {
		return false
	}
	p := r.URL.Path
	return loc.Path != p && (loc.Path == cleanPath(p) || loc.Path == p+"/" || loc.Path+"/" == p)
}

// cleanPath is the path ServeMux and gorilla/mux redirect to: path.Clean, with
// the trailing slash kept.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := pathpkg.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}

// A pattern stack ending at a subrouter is an unmatched request or a
// middleware response, not an endpoint. Preserve trailing slashes for real
// endpoints; chi.Context.RoutePattern trims them.
func chiPattern(ctx *chi.Context) string {
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
	return joinChi(patterns...)
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

// chiFind is routes.Find with mount stubs resolved. Find stops at the stub
// for /users, so the route that serves it is the subrouter's own / route,
// and a subrouter without one leaves the request unmatched.
func chiFind(routes chi.Routes, method, path string) string {
	pattern := routes.Find(chi.NewRouteContext(), method, path)
	mount, ok := mountOf(routes, pattern)
	if !ok {
		return pattern
	}
	sub := chiFind(mount.SubRoutes, method, "/")
	if sub == "" {
		return ""
	}
	return joinChi(mount.Pattern, sub)
}

// joinChi joins a chi pattern stack, dropping the /* each mount leaves.
func joinChi(patterns ...string) string {
	pattern := strings.Join(patterns, "")
	for strings.Contains(pattern, "/*/") {
		pattern = strings.ReplaceAll(pattern, "/*/", "/")
	}
	return pattern
}

// match supplies a fallback when middleware returns before endpoint dispatch.
// Gorilla also keeps its matched route on a request copy the caller cannot see.
func (rec *Recorder) match(r *http.Request) string {
	routes, _ := rec.next.(chi.Routes)
	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.Routes != nil {
		routes = rctx.Routes
	}
	if routes != nil {
		// Find preserves trailing slashes; RoutePattern trims them. Match the
		// escaped path like chi does, so encoded slashes stay in one segment.
		return chiFind(routes, r.Method, cmp.Or(r.URL.RawPath, r.URL.Path))
	}
	if mr, ok := rec.next.(*mux.Router); ok {
		var match mux.RouteMatch
		if mr.Match(r, &match) && match.Route != nil {
			if tpl, err := match.Route.GetPathTemplate(); err == nil {
				return tpl
			}
		}
	}
	return ""
}

// stdPattern extracts the operation path from an http.ServeMux pattern.
// Method-less subtree patterns can be outer mounts, so ignore them. A
// method-qualified subtree is a specout catch-all: it is not in the paths
// object, but its declared statuses still count.
func stdPattern(p string) string {
	if _, path, ok := strings.Cut(p, " "); ok {
		return path
	}
	if strings.HasSuffix(p, "/") {
		return ""
	}
	return p
}
