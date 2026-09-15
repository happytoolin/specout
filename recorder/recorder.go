package recorder

import (
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

// Recorder wraps a router and observes status codes and bodies per route,
// keyed by the matched pattern.
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
	pattern := matchedPattern(r)
	if pattern == "" {
		if routes, ok := rec.next.(chi.Routes); ok {
			rctx := chi.NewRouteContext()
			if routes.Match(rctx, r.Method, r.URL.Path) {
				pattern = rctx.RoutePattern()
			}
		}
	}
	rw := &observingWriter{ResponseWriter: w}
	rec.next.ServeHTTP(rw, r)

	key := specout.RouteKey{Method: r.Method, Path: specout.NormalizePath(pattern)}
	rec.mu.Lock()
	if rec.codes[key] == nil {
		rec.codes[key] = make(map[int]bool)
	}
	rec.codes[key][rw.code] = true
	rec.mu.Unlock()
}

// matchedPattern finds the full route pattern for the request: chi first,
// then std r.Pattern.
func matchedPattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return ""
}

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
