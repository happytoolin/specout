package specout

import (
	"net/http"
	"reflect"
)

// Router is the adapter of one router: http.ServeMux, chi, Gin and the
// others each have one. Write one for another router in a few lines.
type Router interface {
	// Add registers h on the router for method and path, in the router's own
	// syntax, and returns the full path that the router serves, in OpenAPI
	// syntax: {name} for a parameter, {name...} for a catch-all. Add copies the
	// router's path parameters into r.PathValue before h runs. A path that the
	// router or the adapter rejects panics.
	Add(method, path string, h http.Handler) string
	// Routes returns the live route table of the router, for Check, with each
	// path in OpenAPI syntax. A route with an empty Method serves every
	// method.
	Routes() ([]Route, error)
}

// Route is one entry of a router's route table.
type Route struct{ Method, Path string }

// Binder registers specout handlers on a router, and records each one in the
// document. Bind returns it.
type Binder struct {
	d *Generator
	r Router
}

// Bind returns the binder for d on r, the adapter of a router, as in
// specout.Bind(doc, specoutchi.New(r)).
func Bind(d *Generator, r Router) *Binder {
	if d == nil || isNil(r) {
		panic("specout: Bind needs a generator and a router adapter")
	}
	return &Binder{d: d, r: r}
}

// isNil reports a nil r, also a nil pointer in the interface, which r == nil
// does not catch.
func isNil(r Router) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// Handle registers h for method on path, in the router's own syntax.
func (b *Binder) Handle[Req, Res any](method, path string, h Handler[Req, Res]) {
	b.d.bind(method, path, recOf(h), func(next http.Handler) string {
		return b.r.Add(method, path, next)
	})
}

// Get registers a GET route on path.
func (b *Binder) Get[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodGet, path, h)
}

// Head registers a HEAD route on path.
func (b *Binder) Head[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodHead, path, h)
}

// Post registers a POST route on path.
func (b *Binder) Post[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodPost, path, h)
}

// Put registers a PUT route on path.
func (b *Binder) Put[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodPut, path, h)
}

// Patch registers a PATCH route on path.
func (b *Binder) Patch[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodPatch, path, h)
}

// Delete registers a DELETE route on path.
func (b *Binder) Delete[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodDelete, path, h)
}

// Options registers an OPTIONS route on path.
func (b *Binder) Options[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodOptions, path, h)
}

// Trace registers a TRACE route on path.
func (b *Binder) Trace[Req, Res any](path string, h Handler[Req, Res]) {
	b.Handle(http.MethodTrace, path, h)
}

// Check reports the routes of r that d does not document: routes that no
// Binder registered and no Document call names, and routes that serve every
// method. A skip that ends in /* skips every path below it. Skip the spec
// endpoint itself, as in Check(doc, specoutgin.New(engine), "/openapi.json").
func Check(d *Generator, r Router, skips ...string) error {
	if d == nil || isNil(r) {
		panic("specout: Check needs a generator and a router adapter")
	}
	routes, err := r.Routes()
	if err != nil {
		return err
	}
	return d.checkRoutes(routes, skips)
}
