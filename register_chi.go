package specout

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Chi returns the chi binder for d on r: verb methods register handlers and
// metadata in one step; Adopt and RequireDocumented cover existing routers.
func Chi(d *Generator, r chi.Router) *ChiRouter { return &ChiRouter{d: d, r: r} }

// ChiRouter registers specout handlers on a chi.Router. Relative patterns
// (Route/Mount groups) resolve at build time from a walk of the live router.
type ChiRouter struct {
	d *Generator
	r chi.Router
}

// source makes the generator walk this router at build time.
func (c *ChiRouter) source() { c.d.addSource(chiWalkSource{c.r}) }

func (c *ChiRouter) register(method, pattern string, rec routeRecord) {
	rec.method, rec.pattern = method, pattern
	c.r.Method(method, pattern, http.HandlerFunc(rec.fn))
	c.d.register(rec)
}

// The eight verbs differ only in the method token.
func (c *ChiRouter) Get[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodGet, p, recOf(h))
}

func (c *ChiRouter) Head[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodHead, p, recOf(h))
}

func (c *ChiRouter) Post[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodPost, p, recOf(h))
}

func (c *ChiRouter) Put[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodPut, p, recOf(h))
}

func (c *ChiRouter) Patch[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodPatch, p, recOf(h))
}

func (c *ChiRouter) Delete[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodDelete, p, recOf(h))
}

func (c *ChiRouter) Options[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodOptions, p, recOf(h))
}

func (c *ChiRouter) Trace[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodTrace, p, recOf(h))
}

// Adopt walks an already-built chi router: routes whose handler specout
// does not know (plain r.Get strays) are reported as errors; skips use
// SkipRule patterns like /healthz or /debug/*.
func (c *ChiRouter) Adopt(skips ...SkipRule) error {
	s := strayScan{d: c.d, skips: skips}
	err := chi.Walk(c.r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		s.collect(route, method, handler)
		return nil
	})
	if err != nil {
		return err
	}
	// Adopt is the only place the walk is registered, so it must happen even
	// when strays are found: otherwise one undocumented route leaves every
	// relative pattern unresolved and the spec build dies with the wrong error.
	c.source()
	return s.err()
}

// RequireDocumented fails the test when the chi router holds endpoint
// routes specout never registered.
func (c *ChiRouter) RequireDocumented(t TestingT, s ...SkipRule) {
	requireDocumented(t, c.Adopt(s...))
}

// chiWalkSource adapts chi.Router to routeSource. chi.Walk reports
// composed full patterns; catch-alls keep their trailing wildcard.
type chiWalkSource struct{ r chi.Router }

func (s chiWalkSource) walk(fn func(method, pattern string, h http.Handler)) {
	_ = chi.Walk(s.r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		fn(method, canonicalPath(route), handler)
		return nil
	})
}
