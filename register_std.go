package specout

import (
	"net/http"
	"strconv"
	"strings"
)

// Std returns the std-mux binder for d on mux. Patterns are absolute
// "METHOD /path" with method tokens and wildcards; no walk needed.
func Std(d *Generator, mux *http.ServeMux) *StdRouter {
	if d == nil || mux == nil {
		panic("specout: Std needs a generator and an *http.ServeMux")
	}
	return &StdRouter{d: d, mux: mux}
}

// StdRouter registers specout handlers on an http.ServeMux. Patterns are
// absolute ("METHOD /path"); no walk is needed.
type StdRouter struct {
	d   *Generator
	mux *http.ServeMux
}

// Handle registers h for "METHOD /path" and records its metadata. net/http
// serves /a/{$} for /a/ only and /a/ for the paths below it, but both are the
// OpenAPI path /a/, so the build fails when both are registered here. Register
// the other one on the mux itself, and give the recorder a specout.Skip for
// its pattern.
func (s *StdRouter) Handle[Req, Res any](pattern string, h Handler[Req, Res]) {
	method, path, ok := strings.Cut(pattern, " ")
	// one space, absolute path: "GET  /x" would document a path with a
	// leading blank, and net/http panics on "GET x" with its own message.
	if !ok || !strings.HasPrefix(path, "/") || strings.Contains(path, " ") {
		panic("specout: pattern " + strconv.Quote(pattern) + " is not valid; pass Handle \"METHOD /path\", " +
			"and pass Get, Post and the other verb methods only /path")
	}
	checkMethod(method, path)
	if msg := pathCharProblem(path); msg != "" {
		panic("specout: " + method + " " + path + ": " + msg)
	}
	rec := recOf(h)
	rec.method, rec.pattern = method, path
	rec.full = canonicalPath(path)
	rec.absolute = true
	rec.literalStars = true
	// A path that ends in / without {$} matches every path below it, as
	// /files/{path...} does: OpenAPI has no form for it.
	rec.catchAll = strings.HasSuffix(path, "/")
	s.d.addRoute(rec, func(rec *routeRecord) {
		defer func() { muxPanic(rec.label(), recover()) }()
		s.mux.Handle(pattern, rec)
	})
}

// verbPattern is the ServeMux pattern of a verb method: the method, a space and
// the path. A verb method takes only the path.
func verbPattern(method, p string) string {
	if !strings.HasPrefix(p, "/") {
		panic("specout: " + method + " " + strconv.Quote(p) + ": the path must start with /; " +
			"pass only the path to " + verbName(method) + ", such as \"/items\"")
	}
	return method + " " + p
}

// verbName is the name of the verb method for method, such as Get for GET.
func verbName(method string) string {
	return method[:1] + strings.ToLower(method[1:])
}

// muxPanic names the route in a panic of http.ServeMux, when there is one.
// The ServeMux message names the specout file that called Handle, not the
// caller, so a conflict keeps only its reason.
func muxPanic(where string, v any) {
	err, ok := v.(error)
	if !ok {
		if v != nil {
			panic(v)
		}
		return
	}
	msg := err.Error()
	if _, reason, cut := strings.Cut(msg, ":\n"); cut && strings.Contains(msg, "conflicts with") {
		panic("specout: " + where + ": " + reason + "; register each route once, or change one of the paths")
	}
	panic("specout: " + where + ": http.ServeMux rejects the pattern: " + msg)
}

// Get registers a GET route on p.
func (s *StdRouter) Get[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodGet, p), h)
}

// Head registers a HEAD route on p.
func (s *StdRouter) Head[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodHead, p), h)
}

// Post registers a POST route on p.
func (s *StdRouter) Post[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodPost, p), h)
}

// Put registers a PUT route on p.
func (s *StdRouter) Put[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodPut, p), h)
}

// Patch registers a PATCH route on p.
func (s *StdRouter) Patch[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodPatch, p), h)
}

// Delete registers a DELETE route on p.
func (s *StdRouter) Delete[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodDelete, p), h)
}

// Options registers an OPTIONS route on p.
func (s *StdRouter) Options[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodOptions, p), h)
}

// Trace registers a TRACE route on p.
func (s *StdRouter) Trace[Req, Res any](p string, h Handler[Req, Res]) {
	s.Handle(verbPattern(http.MethodTrace, p), h)
}
