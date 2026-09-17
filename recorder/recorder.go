package recorder

import (
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
)

// Recorder wraps a router and observes status codes per route, keyed by
// the matched pattern. Method-less matches (404s) record nothing.
type Recorder struct {
	next http.Handler
	// skips: routes whose observations are not API drift, e.g. the spec
	// endpoint mounted on the same router.
	skips []specout.SkipRule

	mu    sync.Mutex
	codes map[specout.RouteKey]map[int]bool
}

// New wraps next (the app router) for observation. A route matching a skip
// is served but not recorded, so a spec endpoint on the same router does not
// read as an undocumented operation.
//
// Pattern capture covers chi, gorilla/mux and net/http.ServeMux. A router
// specout reaches only through Document[Req,Res] (echo, fiber, gin) exposes
// no pattern here, so Verify reports its routes as never produced.
func New(next http.Handler, skips ...specout.SkipRule) *Recorder {
	return &Recorder{next: next, skips: skips, codes: make(map[specout.RouteKey]map[int]bool)}
}

func (rec *Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// chi populates RouteContext before dispatch completes and resets it
	// after. If the request reaches us unwrapped, match manually.
	pattern := ""
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		pattern = rctx.RoutePattern()
	}
	if pattern == "" {
		if routes, ok := rec.next.(chi.Routes); ok {
			rctx := chi.NewRouteContext()
			if routes.Match(rctx, r.Method, r.URL.Path) {
				pattern = rctx.RoutePattern()
			}
		}
	}
	// gorilla puts the matched route on a request copy the caller never
	// sees, so CurrentRoute(r) is always nil after dispatch: match here.
	if pattern == "" {
		if mr, ok := rec.next.(*mux.Router); ok {
			var match mux.RouteMatch
			if mr.Match(r, &match) && match.Route != nil {
				if tpl, err := match.Route.GetPathTemplate(); err == nil {
					pattern = tpl
				}
			}
		}
	}
	rw := &observingWriter{ResponseWriter: w}
	rec.next.ServeHTTP(rw, r)

	// std populates r.Pattern during dispatch; read after. A pattern ending
	// in "/" is a subtree mount ("/api/v3/" in front of the app router), not
	// one operation: a request the app router did not match must not be
	// keyed under the mount, or every 404 and 405 reads as drift.
	if pattern == "" && r.Pattern != "" {
		p := r.Pattern
		if _, path, ok := strings.Cut(p, " "); ok {
			p = path
		}
		if !strings.HasSuffix(p, "/") {
			pattern = p
		}
	}

	// unmatched: no key, no drift entry
	if pattern == "" {
		return
	}
	for _, s := range rec.skips {
		if s.Matches(pattern) {
			return
		}
	}

	key := specout.NewRouteKey(r.Method, pattern)
	rec.mu.Lock()
	if rec.codes[key] == nil {
		rec.codes[key] = make(map[int]bool)
	}
	rec.codes[key][rw.code] = true
	rec.mu.Unlock()
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
