// Package recorder wraps a router and reports the status codes the router
// actually returned per route, so a test can compare live behaviour against
// the documented spec.
package recorder

import (
	"cmp"
	"context"
	"net/http"
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
}

// New wraps next (the app router) for observation: a route matching a skip is
// served but not recorded. Pattern capture covers chi, gorilla/mux and
// net/http.ServeMux; a router specout reaches only through Document[Req,Res]
// (echo, fiber, gin) exposes no pattern, so Verify reports its routes as never
// produced.
func New(next http.Handler, skips ...specout.SkipRule) *Recorder {
	return &Recorder{next: next, skips: skips, codes: map[specout.RouteKey]map[int]bool{}}
}

func (rec *Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	// unmatched, or a skipped route: no key, no drift entry
	if pattern == "" || slices.ContainsFunc(rec.skips, func(s specout.SkipRule) bool { return s.Matches(pattern) }) {
		return
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	key := specout.NewRouteKey(method, pattern)
	if rec.codes[key] == nil {
		rec.codes[key] = map[int]bool{}
	}
	rec.codes[key][cmp.Or(rw.Status(), http.StatusOK)] = true
}

// A pattern stack ending at a subrouter is an unmatched request or a
// middleware response, not an endpoint. Preserve trailing slashes for real
// endpoints; chi.Context.RoutePattern trims them.
func chiPattern(ctx *chi.Context) string {
	routes := ctx.Routes
	for _, pattern := range ctx.RoutePatterns {
		if routes == nil {
			return ""
		}
		var next chi.Routes
		for _, route := range routes.Routes() {
			if route.Pattern == pattern {
				next = route.SubRoutes
				break
			}
		}
		routes = next
	}
	if routes != nil {
		return ""
	}
	pattern := strings.Join(ctx.RoutePatterns, "")
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
		return routes.Find(chi.NewRouteContext(), r.Method, cmp.Or(r.URL.RawPath, r.URL.Path))
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
// method-qualified subtree is a documented operation, including its slash.
func stdPattern(p string) string {
	if _, path, ok := strings.Cut(p, " "); ok {
		return path
	}
	if strings.HasSuffix(p, "/") {
		return ""
	}
	return p
}
