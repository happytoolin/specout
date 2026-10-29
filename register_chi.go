package specout

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Chi returns the chi binder for d on r: verb methods register handlers and
// metadata in one step; Adopt and RequireDocumented cover existing routers.
func Chi(d *Generator, r chi.Router) *ChiRouter {
	if d == nil || nilRouter(r) {
		panic("specout: Chi needs a generator and a chi.Router")
	}
	return &ChiRouter{d: d, r: r}
}

// nilRouter reports a nil router, also a nil *chi.Mux in the interface, which
// r == nil does not catch.
func nilRouter(r chi.Router) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// ChiRouter registers specout handlers on a chi.Router. Relative patterns
// (Route/Mount groups) resolve at build time from a walk of the live router.
type ChiRouter struct {
	d *Generator
	r chi.Router
}

// source makes the generator walk this router at build time.
func (c *ChiRouter) source() { c.d.addSource(chiWalkSource{c.r}) }

func (c *ChiRouter) register(method, pattern string, rec routeRecord) {
	// chi matches /a//b and /a/./b only as written, but clients clean them
	// and the document would show /a/b.
	if msg := checkSegments(pattern); msg != "" {
		panic("specout: chi route " + method + " " + pattern + ": " + msg)
	}
	rec.method, rec.pattern = method, pattern
	c.d.addRoute(rec, func(rec *routeRecord) {
		c.r.Method(method, pattern, rec)
	})
}

// Get registers a GET route on p.
func (c *ChiRouter) Get[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodGet, p, recOf(h))
}

// Head registers a HEAD route on p.
func (c *ChiRouter) Head[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodHead, p, recOf(h))
}

// Post registers a POST route on p.
func (c *ChiRouter) Post[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodPost, p, recOf(h))
}

// Put registers a PUT route on p.
func (c *ChiRouter) Put[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodPut, p, recOf(h))
}

// Patch registers a PATCH route on p.
func (c *ChiRouter) Patch[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodPatch, p, recOf(h))
}

// Delete registers a DELETE route on p.
func (c *ChiRouter) Delete[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodDelete, p, recOf(h))
}

// Options registers an OPTIONS route on p.
func (c *ChiRouter) Options[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodOptions, p, recOf(h))
}

// Trace registers a TRACE route on p.
func (c *ChiRouter) Trace[Req, Res any](p string, h Handler[Req, Res]) {
	c.register(http.MethodTrace, p, recOf(h))
}

// Adopt walks an already-built chi router: routes whose handler specout
// does not know (plain r.Get strays) are reported as errors; skips use
// SkipRule patterns like /healthz or /debug/*.
func (c *ChiRouter) Adopt(skips ...SkipRule) error {
	s := strayScan{d: c.d, skips: skips}
	err := chi.Walk(c.r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		s.collect(chiRoute(route), method, handler)
		return nil
	})
	if err != nil {
		return fmt.Errorf("specout: walk chi router: %w", err)
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

// chiWalkSource adapts chi.Routes to routeSource. chi.Walk reports
// composed full patterns; catch-alls keep their trailing wildcard.
type chiWalkSource struct{ r chi.Routes }

func (s chiWalkSource) walk(fn func(method, pattern string, h http.Handler)) {
	_ = chi.Walk(s.r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		fn(method, canonicalPath(chiRoute(route)), handler)
		return nil
	})
}

// chiRoute removes the /* that chi.Walk leaves where a router is mounted.
// chi.Walk replaces each /*/ once, so a mount at / inside a group leaves
// /v1/*/x behind, which would read as a catch-all and drop the route. chi
// allows * only at the end of a pattern, so a /*/ is always a mount.
func chiRoute(route string) string {
	for strings.Contains(route, "/*/") {
		route = strings.ReplaceAll(route, "/*/", "/")
	}
	return route
}

// checkChiMount fails when the chi router that served the spec request serves
// a chi registration under a prefix the document does not declare. That is an
// Adopt on a router mounted inside it: the walk saw /x, the client must call
// /api/x. A prefix that ends a Config.Servers path is declared, and so is any
// prefix when a server URL is templated. Caller holds d.mu.
func (d *Generator) checkChiMount() error {
	if d.outer == nil || slices.Contains(d.sources, d.outer) {
		return nil
	}
	hits := d.walkHits([]routeSource{d.outer})
	for _, rec := range d.records {
		if rec.absolute || rec.full == "" {
			continue
		}
		for _, p := range hits[rec] {
			prefix, ok := strings.CutSuffix(p, rec.full)
			if !ok || prefix == "" || d.serverDeclares(prefix) {
				continue
			}
			return fmt.Errorf(
				"specout: %s %s is served at %s, under a router mounted at %s; adopt the outermost "+
					"router, or end a Config.Servers URL with %s",
				rec.method, rec.full, p, prefix, prefix,
			)
		}
	}
	return nil
}

// serverDeclares reports whether a Config.Servers URL accounts for a mount
// prefix: its path ends with the prefix, or the URL is templated.
func (d *Generator) serverDeclares(prefix string) bool {
	for _, s := range d.cfg.Servers {
		if strings.Contains(s.URL, "{") {
			return true
		}
		u, err := url.Parse(s.URL)
		if err != nil {
			continue
		}
		// prefix starts with /, so the match is whole segments
		if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), prefix) {
			return true
		}
	}
	return false
}
