package specout

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Chi returns the chi binder for d on r: verb methods register handlers and
// metadata in one step; Adopt and RequireDocumented cover existing routers.
func Chi(d *Generator, r chi.Router) *ChiRouter {
	return &ChiRouter{d: d, r: r}
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
	rec.method, rec.pattern = method, pattern
	c.r.Method(method, pattern, http.HandlerFunc(rec.fn))
	c.d.register(rec)
}

func (c *ChiRouter) Get[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodGet, pattern, recOf(h))
}

func (c *ChiRouter) Head[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodHead, pattern, recOf(h))
}

func (c *ChiRouter) Post[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodPost, pattern, recOf(h))
}

func (c *ChiRouter) Put[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodPut, pattern, recOf(h))
}

func (c *ChiRouter) Patch[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodPatch, pattern, recOf(h))
}

func (c *ChiRouter) Delete[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodDelete, pattern, recOf(h))
}

func (c *ChiRouter) Options[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodOptions, pattern, recOf(h))
}

func (c *ChiRouter) Trace[Req, Res any](pattern string, h Handler[Req, Res]) {
	c.register(http.MethodTrace, pattern, recOf(h))
}

// Adopt walks an already-built chi router: routes whose handler specout
// does not know (plain r.Get strays) are reported as errors; skips use
// SkipRule patterns like /healthz or /debug/*.
func (c *ChiRouter) Adopt(skips ...SkipRule) error {
	var unknown []string
	err := chi.Walk(c.r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		for _, s := range skips {
			if s.Matches(route) {
				return nil
			}
		}
		if handler == http.Handler(c.d) {
			return nil
		}
		ptr := reflect.ValueOf(handler).Pointer()
		if !c.d.lookup(ptr) {
			unknown = append(unknown, method+" "+route)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Adopt is the only place the walk is registered, so it must happen even
	// when strays are found: otherwise one undocumented route leaves every
	// relative pattern unresolved and the spec build dies with the wrong error.
	c.source()
	if len(unknown) > 0 {
		return &StrayError{Routes: unknown}
	}
	return nil
}

// RequireDocumented fails the test when the chi router holds endpoint
// routes specout never registered.
func (c *ChiRouter) RequireDocumented(t TestingT, skips ...SkipRule) {
	t.Helper()
	if err := c.Adopt(skips...); err != nil {
		t.Errorf("%v", err)
	}
}

// chiWalkSource adapts chi.Router to routeSource. chi.Walk reports
// composed full patterns; catch-alls keep their trailing wildcard.
type chiWalkSource struct{ r chi.Router }

func (s chiWalkSource) walk(fn func(method, pattern string, h http.Handler)) {
	_ = chi.Walk(s.r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		fn(method, chiCanonical(route), handler)
		return nil
	})
}

// chiCanonical collapses empty segments (Route groups emit "//"); trailing
// slashes are significant: chi walks /a and /a/ as distinct routes.
func chiCanonical(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

// recOf pulls the metadata fields off a Handler into a routeRecord.
func recOf[Req, Res any](h Handler[Req, Res]) routeRecord {
	return routeRecord{
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary,
		operationID: h.OperationID, description: h.Description,
		externalDocs: h.ExternalDocs, reqContentTypes: h.RequestContentTypes,
		raw:        h.Raw,
		deprecated: h.Deprecated, public: h.Public, fn: h.HandlerFunc,
	}
}

// StrayError names routes registered without specout metadata.
type StrayError struct{ Routes []string }

func (e *StrayError) Error() string {
	return "specout: routes not documented: " + strings.Join(e.Routes, ", ")
}
