package specout

import (
	"net/http"
	"strings"
)

// Std returns the std-mux binder for d on mux. Patterns are absolute
// "METHOD /path" with method tokens and wildcards; no walk needed.
func Std(d *Generator, mux *http.ServeMux) *StdRouter { return &StdRouter{d: d, mux: mux} }

// StdRouter registers specout handlers on an http.ServeMux. Patterns are
// absolute ("METHOD /path"); no walk is needed.
type StdRouter struct {
	d   *Generator
	mux *http.ServeMux
}

// Handle registers h for "METHOD /path" and records its metadata.
func (s *StdRouter) Handle[Req, Res any](pattern string, h Handler[Req, Res]) {
	method, path, ok := strings.Cut(pattern, " ")
	// one space, absolute path: "GET  /x" would document a path with a
	// leading blank, and net/http panics on "GET x" with its own message.
	if !ok || !strings.HasPrefix(path, "/") || strings.Contains(path, " ") {
		panic("specout: std pattern must be METHOD /path, got " + pattern)
	}
	checkMethod(method)
	s.mux.HandleFunc(pattern, h.HandlerFunc)
	rec := recOf(h)
	rec.method, rec.pattern = method, path
	rec.full = canonicalPath(path)
	rec.absolute = true
	s.d.register(rec)
}

// Get registers a GET route on p.
func (s *StdRouter) Get[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodGet+" "+p, h)
}

// Head registers a HEAD route on p.
func (s *StdRouter) Head[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodHead+" "+p, h)
}

// Post registers a POST route on p.
func (s *StdRouter) Post[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodPost+" "+p, h)
}

// Put registers a PUT route on p.
func (s *StdRouter) Put[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodPut+" "+p, h)
}

// Patch registers a PATCH route on p.
func (s *StdRouter) Patch[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodPatch+" "+p, h)
}

// Delete registers a DELETE route on p.
func (s *StdRouter) Delete[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodDelete+" "+p, h)
}

// Options registers an OPTIONS route on p.
func (s *StdRouter) Options[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodOptions+" "+p, h)
}

// Trace registers a TRACE route on p.
func (s *StdRouter) Trace[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(http.MethodTrace+" "+p, h)
}
