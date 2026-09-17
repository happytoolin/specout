package recorder

import (
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
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
	rw := &observingWriter{ResponseWriter: w}
	rec.next.ServeHTTP(rw, r)

	// std populates r.Pattern during dispatch, so read it only after.
	if pattern == "" {
		pattern = stdPattern(r.Pattern)
	}
	// unmatched, or a skipped route: no key, no drift entry
	if pattern == "" || slices.ContainsFunc(rec.skips, func(s specout.SkipRule) bool { return s.Matches(pattern) }) {
		return
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	key := specout.NewRouteKey(r.Method, pattern)
	if rec.codes[key] == nil {
		rec.codes[key] = map[int]bool{}
	}
	rec.codes[key][rw.code] = true
}

// match resolves the pattern r dispatches to. chi sets the RouteContext before
// dispatch and clears it after, and a request reaching us unwrapped is matched
// manually; gorilla keeps the matched route on a request copy the caller never
// sees, so match here too.
func (rec *Recorder) match(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	if routes, ok := rec.next.(chi.Routes); ok {
		if rctx := chi.NewRouteContext(); routes.Match(rctx, r.Method, r.URL.Path) {
			return rctx.RoutePattern()
		}
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

// stdPattern extracts the operation path from an http.ServeMux pattern. A
// pattern ending in "/" is a subtree mount ("/api/v3/" in front of the app
// router), not one operation: a request the app router did not match must not
// be keyed under the mount, or every 404 and 405 reads as drift.
func stdPattern(p string) string {
	if _, path, ok := strings.Cut(p, " "); ok {
		p = path
	}
	if strings.HasSuffix(p, "/") {
		return ""
	}
	return p
}

// observingWriter records the status code; Unwrap keeps ResponseController,
// Flusher and Hijacker working through the wrapper.
type observingWriter struct {
	http.ResponseWriter
	code int
}

func (w *observingWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *observingWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *observingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
