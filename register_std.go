package specout

import (
	"net/http"
	"strings"
)

// Std returns the std-mux binder for d on mux. Patterns are absolute
// "METHOD /path" with method tokens and wildcards; no walk needed.
func Std(d *Generator, mux *http.ServeMux) *StdRouter {
	return &StdRouter{d: d, mux: mux}
}

type StdRouter struct {
	d   *Generator
	mux *http.ServeMux
}

// Handle registers h for "METHOD /path" and records its metadata.
func (s *StdRouter) Handle[Req, Res any](pattern string, h Handler[Req, Res]) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		panic("specout: std pattern must be " + "METHOD /path")
	}
	checkMethod(method)
	s.mux.HandleFunc(pattern, h.HandlerFunc)
	rec := recOf(h)
	rec.method, rec.pattern = method, path
	rec.full = stdCanonical(path)
	rec.absolute = true
	s.d.register(rec)
}

func (s *StdRouter) Get[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodGet+" "+pattern, h)
}

func (s *StdRouter) Head[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodHead+" "+pattern, h)
}

func (s *StdRouter) Post[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodPost+" "+pattern, h)
}

func (s *StdRouter) Put[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodPut+" "+pattern, h)
}

func (s *StdRouter) Patch[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodPatch+" "+pattern, h)
}

func (s *StdRouter) Delete[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodDelete+" "+pattern, h)
}

func (s *StdRouter) Options[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodOptions+" "+pattern, h)
}

func (s *StdRouter) Trace[Req, Res any](pattern string, h Handler[Req, Res]) {
	s.Handle(http.MethodTrace+" "+pattern, h)
}

// stdCanonical maps std wildcard forms to OpenAPI paths: {$} becomes /.
func stdCanonical(p string) string {
	return strings.TrimSuffix(p, "{$}")
}

// checkMethod panics on non-uppercase method tokens (finding 17).
func checkMethod(method string) {
	if method == strings.ToUpper(method) && method != "" {
		return
	}
	panic("specout: method token must be uppercase, got " + method)
}
