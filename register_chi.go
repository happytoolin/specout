package specout

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5"
)

// registerChi mounts the raw handler on r and records its metadata. Pattern
// may be relative (groups/subrouters); the build-time walk resolves full paths.
func (d *Generator) registerChi(r chi.Router, method, pattern string, rec routeRecord) {
	known := false
	for _, x := range d.chiRoots {
		if x == r {
			known = true
			break
		}
	}
	// interface comparison of chi.Router values; pointer equality underneath
	if !known {
		d.chiRoots = append(d.chiRoots, r)
	}
	r.Method(method, pattern, http.HandlerFunc(rec.fn))
	d.register(rec)
}

// Adopt walks an already-built chi router: routes whose handler specout does
// not know (plain r.Get strays) are reported as errors; on success the router
// becomes a walk root, so Route/Mount prefixes compose into full paths at
// build time.
func (d *Generator) Adopt(r chi.Router) error {
	var unknown []string
	err := chi.Walk(r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		// skip our own /openapi.json mount: chi wraps it, so match the
		// ServeHTTP method pointer instead of the type
		if handler == http.Handler(d) {
			return nil
		}
		ptr := reflect.ValueOf(handler).Pointer()
		if _, ok := d.lookup(ptr); !ok {
			unknown = append(unknown, method+" "+route)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return fmt.Errorf("specout: routes without specout handlers: %s", strings.Join(unknown, ", "))
	}
	for _, x := range d.chiRoots {
		if x == r {
			return nil
		}
	}
	d.chiRoots = append(d.chiRoots, r)
	return nil
}

// Get registers h on r for GET and records its metadata.
func (d *Generator) Get[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodGet, pattern, routeRecord{
		method: http.MethodGet, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Head registers h on r for HEAD and records its metadata.
func (d *Generator) Head[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodHead, pattern, routeRecord{
		method: http.MethodHead, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Post registers h on r for POST and records its metadata.
func (d *Generator) Post[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodPost, pattern, routeRecord{
		method: http.MethodPost, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Put registers h on r for PUT and records its metadata.
func (d *Generator) Put[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodPut, pattern, routeRecord{
		method: http.MethodPut, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Patch registers h on r for PATCH and records its metadata.
func (d *Generator) Patch[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodPatch, pattern, routeRecord{
		method: http.MethodPatch, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Delete registers h on r for DELETE and records its metadata.
func (d *Generator) Delete[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodDelete, pattern, routeRecord{
		method: http.MethodDelete, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Options registers h on r for OPTIONS and records its metadata.
func (d *Generator) Options[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodOptions, pattern, routeRecord{
		method: http.MethodOptions, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}

// Trace registers h on r for TRACE and records its metadata.
func (d *Generator) Trace[Req, Res any](r chi.Router, pattern string, h Handler[Req, Res]) {
	d.registerChi(r, http.MethodTrace, pattern, routeRecord{
		method: http.MethodTrace, pattern: pattern,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary, deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}
