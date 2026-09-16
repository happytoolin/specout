package specout

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gorilla/mux"
)

// Gorilla returns the gorilla binder for d on r. Templates are absolute;
// {id} and {id:re...} params map straight to OpenAPI path params.
func Gorilla(d *Generator, r *mux.Router) *GorillaRouter {
	return &GorillaRouter{d: d, r: r}
}

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

func (g *GorillaRouter) Get[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodGet, pattern, recOf(h))
}

func (g *GorillaRouter) Head[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodHead, pattern, recOf(h))
}

func (g *GorillaRouter) Post[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodPost, pattern, recOf(h))
}

func (g *GorillaRouter) Put[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodPut, pattern, recOf(h))
}

func (g *GorillaRouter) Patch[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodPatch, pattern, recOf(h))
}

func (g *GorillaRouter) Delete[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodDelete, pattern, recOf(h))
}

func (g *GorillaRouter) Options[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodOptions, pattern, recOf(h))
}

func (g *GorillaRouter) Trace[Req, Res any](pattern string, h Handler[Req, Res]) {
	g.register(http.MethodTrace, pattern, recOf(h))
}

// Adopt walks an already-built gorilla router. Routes without a method
// constraint are all-methods endpoints, not mounts: they fail loud
// (they cannot be documented as one OpenAPI operation) unless skipped.
func (g *GorillaRouter) Adopt(skips ...SkipRule) error {
	var unknown []string
	err := g.r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		for _, s := range skips {
			if matchSkip(s.pattern, tpl) {
				return nil
			}
		}
		methods, err := route.GetMethods()
		if err != nil || len(methods) == 0 {
			unknown = append(unknown, "? "+tpl+" (no method constraint)")
			return nil
		}
		handler := route.GetHandler()
		ptr := reflect.ValueOf(handler).Pointer()
		if !g.d.lookup(ptr) {
			unknown = append(unknown, strings.Join(methods, ",")+" "+tpl)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return &StrayError{Routes: unknown}
	}
	return nil
}

// RequireDocumented fails the test when the gorilla router holds endpoint
// routes specout never registered.
func (g *GorillaRouter) RequireDocumented(t TestingT, skips ...SkipRule) {
	t.Helper()
	if err := g.Adopt(skips...); err != nil {
		t.Errorf("%v", err)
	}
}
