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

	mu    sync.Mutex
	codes map[specout.RouteKey]map[int]bool
}

// New wraps next (the app router) for observation.
func New(next http.Handler) *Recorder {
	return &Recorder{
		next:  next,
		codes: make(map[specout.RouteKey]map[int]bool),
	}
}

func (rec *Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// chi populates RouteContext before dispatch completes and resets it
	// after. If the request reaches us unwrapped, match manually.
	chiPattern := ""
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		chiPattern = rctx.RoutePattern()
	}
	if chiPattern == "" {
		if routes, ok := rec.next.(chi.Routes); ok {
			rctx := chi.NewRouteContext()
			if routes.Match(rctx, r.Method, r.URL.Path) {
				chiPattern = rctx.RoutePattern()
			}
		}
	}
	rw := &observingWriter{ResponseWriter: w}
	rec.next.ServeHTTP(rw, r)

	// gorilla and std populate during dispatch; read after.
	pattern := ""
	pattern = chiPattern
	if pattern == "" {
		if cr := mux.CurrentRoute(r); cr != nil {
			if tpl, err := cr.GetPathTemplate(); err == nil {
				pattern = tpl
			}
		}
	}
	if pattern == "" && r.Pattern != "" {
		pattern = r.Pattern
		if _, path, ok := strings.Cut(pattern, " "); ok {
			pattern = path
		}
	}

	// unmatched: no key, no drift entry
	if pattern == "" {
		return
	}

	// HEAD requests match a declared GET: same handler, same status set.
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}

	key := specout.RouteKey{Method: method, Path: driftKey(pattern)}
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

// driftKey mirrors specout served-path canonicalization: chi collapses
// /a and /a/ to the same RoutePattern, so trim one trailing slash.
func driftKey(p string) string {
	if len(p) > 1 {
		return strings.TrimSuffix(p, "/")
	}
	return p
}
