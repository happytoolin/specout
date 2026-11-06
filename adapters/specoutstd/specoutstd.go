// Package specoutstd is the specout adapter for http.ServeMux. Paths use
// ServeMux syntax: {id}, {path...} and {$}. ServeMux sets r.PathValue itself.
// To check status codes in tests, wrap the mux with recorder.New.
package specoutstd

import (
	"errors"
	"net/http"
	"strings"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/bridge"
)

// Router is the adapter of one http.ServeMux.
type Router struct{ mux *http.ServeMux }

// New returns the adapter of mux, for specout.Bind.
func New(mux *http.ServeMux) *Router {
	if mux == nil {
		panic("specoutstd: New needs an *http.ServeMux")
	}
	return &Router{mux: mux}
}

// Add registers h on the mux as "METHOD path". A path that ends in / serves
// every path below it, as {path...} does, so it is not in the document.
// Register /a/{$} for the path /a/ itself.
func (s *Router) Add(method, path string, h http.Handler) string {
	defer func() { muxPanic(recover()) }()
	s.mux.Handle(method+" "+path, h)
	return bridge.MuxPath(path)
}

// Routes returns an error: http.ServeMux does not list its routes. Wrap the
// mux with recorder.New to find routes that are not in the document.
func (*Router) Routes() ([]specout.Route, error) {
	return nil, errors.New("specoutstd: http.ServeMux does not list its routes; " +
		"wrap the mux with recorder.New to find routes that are not documented")
}

// muxPanic keeps only the reason of a ServeMux panic. The ServeMux message
// names the file of this adapter, not the caller; the binder names the route.
func muxPanic(v any) {
	err, ok := v.(error)
	if !ok {
		if v != nil {
			panic(v)
		}
		return
	}
	msg := err.Error()
	if _, reason, cut := strings.Cut(msg, ":\n"); cut && strings.Contains(msg, "conflicts with") {
		panic(reason + "; register each route once, or change one of the paths")
	}
	panic("http.ServeMux rejects the pattern: " + msg)
}
