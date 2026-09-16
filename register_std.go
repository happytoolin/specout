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
	// one space, absolute path: "GET  /x" would document a path with a
	// leading blank, and net/http panics on "GET x" with its own message.
	if !ok || !strings.HasPrefix(path, "/") || strings.Contains(path, " ") {
		panic("specout: std pattern must be METHOD /path, got " + pattern)
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

// checkMethod panics on a method token OpenAPI cannot name (finding 17). A
// path-item object allows only these keys, so a token like CONNECT or a
// lowercase typo would make the emitted document invalid.
func checkMethod(method string) {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete,
		http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace:
		return
	}
	panic("specout: method must be one of GET/HEAD/POST/PUT/PATCH/DELETE/OPTIONS/TRACE, got " + method)
}
