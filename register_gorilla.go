package specout

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// Gorilla returns the gorilla binder for d on r. Templates are absolute;
// {id} and {id:re...} params map straight to OpenAPI path params.
func Gorilla(d *Generator, r *mux.Router) *GorillaRouter { return &GorillaRouter{d: d, r: r} }

// GorillaRouter registers specout handlers on a gorilla/mux router. Templates
// are absolute, so Register needs no walk.
type GorillaRouter struct {
	d *Generator
	r *mux.Router
}

func (g *GorillaRouter) register(method, pattern string, rec routeRecord) {
	rec.method, rec.pattern = method, pattern
	// gorilla folds subrouter PathPrefix into the route's own template when
	// the route is created, so the composed path is available right away.
	if tpl, err := g.r.HandleFunc(pattern, rec.fn).Methods(method).GetPathTemplate(); err == nil {
		rec.full = tpl
	}
	rec.absolute = true
	g.d.register(rec)
}

// Get registers a GET route on p.
func (g *GorillaRouter) Get[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodGet, p, recOf(h))
}

// Head registers a HEAD route on p.
func (g *GorillaRouter) Head[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodHead, p, recOf(h))
}

// Post registers a POST route on p.
func (g *GorillaRouter) Post[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodPost, p, recOf(h))
}

// Put registers a PUT route on p.
func (g *GorillaRouter) Put[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodPut, p, recOf(h))
}

// Patch registers a PATCH route on p.
func (g *GorillaRouter) Patch[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodPatch, p, recOf(h))
}

// Delete registers a DELETE route on p.
func (g *GorillaRouter) Delete[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodDelete, p, recOf(h))
}

// Options registers an OPTIONS route on p.
func (g *GorillaRouter) Options[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodOptions, p, recOf(h))
}

// Trace registers a TRACE route on p.
func (g *GorillaRouter) Trace[Req, Res any](p string, h Handler[Req, Res]) {
	g.register(http.MethodTrace, p, recOf(h))
}

// Adopt walks an already-built gorilla router. Routes without a method
// constraint are all-methods endpoints, not mounts: they fail loud
// (they cannot be documented as one OpenAPI operation) unless skipped.
func (g *GorillaRouter) Adopt(skips ...SkipRule) error {
	s := strayScan{d: g.d, skips: skips}
	err := g.r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		if err != nil {
			return fmt.Errorf("specout: read route template: %w", err)
		}
		// A route with no handler is a PathPrefix/Subrouter mount, not an
		// endpoint: skip it like chi skips Mount stubs.
		h := route.GetHandler()
		if h == nil {
			return nil
		}
		methods, err := route.GetMethods()
		if err != nil {
			methods = nil
		}
		s.collect(tpl, strings.Join(methods, ","), h)
		return nil
	})
	if err != nil {
		return fmt.Errorf("specout: walk gorilla router: %w", err)
	}
	return s.err()
}

// RequireDocumented fails the test when the gorilla router holds endpoint
// routes specout never registered.
func (g *GorillaRouter) RequireDocumented(t TestingT, s ...SkipRule) {
	requireDocumented(t, g.Adopt(s...))
}
