package specout

import (
	"fmt"
	"net/http"
	"regexp/syntax"
	"slices"
	"strings"

	"github.com/gorilla/mux"
)

// Gorilla returns the gorilla binder for d on r. Templates are absolute;
// {id} and {id:re...} params map straight to OpenAPI path params.
func Gorilla(d *Generator, r *mux.Router) *GorillaRouter {
	if d == nil || r == nil {
		panic("specout: Gorilla needs a generator and a *mux.Router")
	}
	return &GorillaRouter{d: d, r: r}
}

// GorillaRouter registers specout handlers on a gorilla/mux router. Templates
// are absolute, so Register needs no walk.
type GorillaRouter struct {
	d *Generator
	r *mux.Router
}

func (g *GorillaRouter) register(method, pattern string, rec routeRecord) {
	rec.method, rec.pattern = method, pattern
	rec.absolute = true
	rec.literalStars = true
	g.d.addRoute(rec, func(rec *routeRecord) {
		// gorilla keeps a route with a bad template and never matches it, so
		// fail with gorilla's reason rather than document a dead path.
		tpl, err := g.r.Handle(pattern, rec).Methods(method).GetPathTemplate()
		if err != nil {
			panic(fmt.Sprintf("specout: gorilla route %s %s: %v", method, pattern, err))
		}
		// gorilla folds subrouter prefixes into the route template.
		rec.full = tpl
		rec.catchAll = spansSegments(tpl)
	})
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
		// A route with no handler is a PathPrefix/Subrouter mount, not an
		// endpoint: skip it like chi skips Mount stubs. A handler that is a
		// router is a mount too; Walk visits its routes next.
		h := route.GetHandler()
		if _, mounted := h.(*mux.Router); h == nil || mounted {
			return nil
		}
		if err := route.GetError(); err != nil {
			return fmt.Errorf("specout: gorilla route: %w", err)
		}
		// A route without a path matcher, such as a CORS preflight on
		// Methods("OPTIONS") alone, serves every path. Report it as *, which
		// Skip("*") matches, instead of stopping the walk.
		tpl, err := route.GetPathTemplate()
		if err != nil {
			tpl = "*"
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

// spansSegments reports a template parameter whose regex can match a slash,
// as {path:.*} does. Such a parameter spans path segments, which an OpenAPI
// path parameter cannot, so the route is a catch-all like chi's /*.
func spansSegments(tpl string) bool {
	for _, m := range pathParamRe.FindAllStringSubmatch(tpl, -1) {
		if re, ok := strings.CutPrefix(m[2], ":"); ok && matchesSlash(re) {
			return true
		}
	}
	return false
}

// matchesSlash reports whether the regex can consume a slash anywhere.
func matchesSlash(re string) bool {
	tree, err := syntax.Parse(re, syntax.Perl)
	if err != nil {
		return false // gorilla has already rejected a regex it cannot compile
	}
	return regexpHasSlash(tree)
}

func regexpHasSlash(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	case syntax.OpLiteral:
		return slices.Contains(re.Rune, '/')
	case syntax.OpCharClass:
		for i := 0; i+1 < len(re.Rune); i += 2 {
			if re.Rune[i] <= '/' && '/' <= re.Rune[i+1] {
				return true
			}
		}
		return false
	default:
		return slices.ContainsFunc(re.Sub, regexpHasSlash)
	}
}

// RequireDocumented fails the test when the gorilla router holds endpoint
// routes specout never registered.
func (g *GorillaRouter) RequireDocumented(t TestingT, s ...SkipRule) {
	requireDocumented(t, g.Adopt(s...))
}
