package recorder

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

// Recorder wraps a router and observes status codes and bodies per route,
// keyed by the matched pattern.
type Recorder struct {
	next http.Handler

	mu     sync.Mutex
	codes  map[specout.RouteKey]map[int]bool
	bodies map[specout.RouteKey]map[int][]byte
}

// New wraps next (the app router) for observation.
func New(next http.Handler) *Recorder {
	return &Recorder{
		next:   next,
		codes:  make(map[specout.RouteKey]map[int]bool),
		bodies: make(map[specout.RouteKey]map[int][]byte),
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
	if rw.body != nil {
		if rec.bodies[key] == nil {
			rec.bodies[key] = make(map[int][]byte)
		}
		rec.bodies[key][rw.code] = rw.body
	}
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
	code    int
	body    []byte
	capture bool
}

func (w *observingWriter) WriteHeader(code int) {
	w.code = code
	w.capture = code >= 200 && code < 300 && code != http.StatusNoContent
	w.ResponseWriter.WriteHeader(code)
}

func (w *observingWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.capture {
		w.body = append(w.body, b...)
	}
	return w.ResponseWriter.Write(b)
}

// Examples returns captured JSON bodies per route and status.
func (rec *Recorder) Examples() map[specout.RouteKey]map[int]any {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make(map[specout.RouteKey]map[int]any)
	for key, byCode := range rec.bodies {
		vals := make(map[int]any)
		for code, raw := range byCode {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				vals[code] = v
			}
		}
		out[key] = vals
	}
	return out
}
