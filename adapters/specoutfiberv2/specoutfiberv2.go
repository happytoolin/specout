// Package specoutfiberv2 binds specout handlers to Fiber v2. Fiber does not
// run net/http handlers natively, so each route runs its handler through
// Fiber's adaptor, which builds an *http.Request for every call; Fiber's path
// parameters arrive as r.PathValue. Adopt checks Fiber's route table for
// routes that carry no specout metadata, and Record feeds the recorder status
// check. For Fiber v3, use specoutfiber.
package specoutfiberv2

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/fiberpath"
	"github.com/happytoolin/specout/recorder"
)

// Router registers specout handlers on a Fiber app or group. Each route is
// documented at its full path, group prefix included.
type Router struct {
	d      *specout.Generator
	r      fiber.Router
	prefix string
}

// New returns the Fiber binder for d on r, a *fiber.App or *fiber.Group.
//
// Use the app that serves requests, or a group of it. Fiber adds the mount
// prefix of a sub-app to its routes only when the app starts, so a Router on
// a sub-app documents its paths without that prefix. Record then reports the
// paths that ran, and recorder.Verify fails.
func New(d *specout.Generator, r fiber.Router) *Router {
	if d == nil {
		panic("specoutfiberv2: New needs a generator")
	}
	switch g := r.(type) {
	case *fiber.App:
		if g != nil {
			return &Router{d: d, r: g}
		}
	case *fiber.Group:
		if g != nil {
			return &Router{d: d, r: g, prefix: g.Prefix}
		}
	}
	panic(fmt.Sprintf("specoutfiberv2: New needs a non-nil *fiber.App or *fiber.Group, got %T", r))
}

// Handle registers h for method on p. Route middleware m runs before h. A GET
// route is registered with Fiber's Get, so it answers HEAD too.
func (b *Router) Handle[Req, Res any](method, p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	full := fiberpath.Join(b.prefix, p)
	documented, _, err := fiberpath.OpenAPI(full)
	if err == nil && fiberpath.RegexEscapeColon(full) {
		err = errors.New(`a Fiber v2 bug reads the \: after a constraint with a backslash as a new parameter; ` +
			`write the constraint without a backslash, such as [0-9] for \d, or use Fiber v3`)
	}
	if err != nil {
		panic(fmt.Sprintf("specoutfiberv2: %s %s: %v", method, full, err))
	}
	// A GET route already answers HEAD, and Fiber would run it first.
	get := []specout.RouteKey{{Method: http.MethodGet, Path: documented}}
	if method == http.MethodHead && b.d.CheckRoutes(get) == nil {
		panic(fmt.Sprintf("specoutfiberv2: HEAD %s after GET %s never runs: Fiber v2 serves HEAD "+
			"through the GET route; register Head before Get", full, full))
	}
	specout.Bind(b.d, method, p, h, func(next http.Handler) string {
		chain := append(slices.Clip(m), serve(next, observed{method: method, path: documented, pattern: full}))
		if method == http.MethodGet {
			b.r.Get(p, chain...)
		} else {
			b.r.Add(method, p, chain...)
		}
		return documented
	})
}

// Get registers a GET route on p. Fiber v2 registers a HEAD route with it, as
// its own Get does; like http.ServeMux, the document lists the GET operation
// only. Fiber appends a route to an earlier one on the same path and runs the
// earlier one first, so register a Head route for the path before its Get: a
// Head after it panics.
func (b *Router) Get[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodGet, p, h, m...)
}

// Head registers a HEAD route on p.
func (b *Router) Head[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodHead, p, h, m...)
}

// Post registers a POST route on p.
func (b *Router) Post[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodPost, p, h, m...)
}

// Put registers a PUT route on p.
func (b *Router) Put[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodPut, p, h, m...)
}

// Patch registers a PATCH route on p.
func (b *Router) Patch[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodPatch, p, h, m...)
}

// Delete registers a DELETE route on p.
func (b *Router) Delete[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodDelete, p, h, m...)
}

// Options registers an OPTIONS route on p.
func (b *Router) Options[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodOptions, p, h, m...)
}

// Trace registers a TRACE route on p.
func (b *Router) Trace[Req, Res any](p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	b.Handle(http.MethodTrace, p, h, m...)
}

// observed is the route a request ran, as the document spells it. serve
// stores it in the request locals for Record. pattern is the Fiber pattern
// the route was registered with.
type observed struct{ method, path, pattern string }

// ran returns the route that ran on the Fiber pattern p. Fiber adds the mount
// prefix of a sub-app to its routes only when the app starts, after the
// Router documented them. Record then reports the path that really ran, so
// Verify names the gap.
func (o observed) ran(p string) observed {
	if p != o.pattern {
		o.path = fiberpath.Table(p)
	}
	return o
}

// pathValue is one path parameter on its way to r.SetPathValue.
type pathValue struct{ name, value string }

// localKey types the request locals this package stores.
type localKey int

const (
	pathValuesKey localKey = iota
	observedKey
)

// serve runs next as the route's last handler. The adaptor builds the
// *http.Request on fasthttp's request context, which reads the request
// locals, so path parameters cross over as a local. They arrive decoded, as
// on http.ServeMux, and copied: Fiber reuses the bytes of its strings after
// the request, and a net/http handler may keep r.PathValue results.
func serve(next http.Handler, route observed) fiber.Handler {
	h := adaptor.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if values, ok := r.Context().Value(pathValuesKey).([]pathValue); ok {
			for _, v := range values {
				r.SetPathValue(v.name, v.value)
			}
		}
		next.ServeHTTP(w, r)
	}))
	return func(c *fiber.Ctx) error {
		decode := !c.App().Config().UnescapePath
		params := c.Route().Params
		values := make([]pathValue, 0, len(params))
		for _, name := range params {
			values = append(values, pathValue{name: name, value: fiberpath.Value(c.Params(name), decode)})
		}
		c.Locals(pathValuesKey, values)
		c.Locals(observedKey, route.ran(c.Route().Path))
		return h(c)
	}
}

// Adopt reports the routes of app that specout cannot document: routes no
// Router registered. Middleware routes are not endpoints and are skipped. So
// are the HEAD routes that Fiber's Get adds beside each GET route: the GET
// route answers for them. A HEAD route registered on its own is reported.
// Skip the spec endpoint itself, as in specout.Skip("/openapi.json").
//
// Adopt also checks the translation of every path against the parameters
// Fiber parsed from it, so syntax specout reads differently fails here.
//
// Adopt also reports a route that never runs. Fiber tries the routes of one
// method in registration order, and an earlier route can take every request
// of a later one: /users/:id takes /users/me, and with the default Config,
// /users takes /Users and /users/. Register the specific route first, or set
// Config.CaseSensitive or Config.StrictRouting. Skipped routes take no part
// in this check.
//
// The routes of a mounted app join app's table, at the mount path, when app
// starts; a Router on the mounted app documents them without that path.
func Adopt(d *specout.Generator, app *fiber.App, skips ...specout.SkipRule) error {
	table := endpoints(app)
	if err := shadowed(app, table, skips); err != nil {
		return err
	}
	routes := make([]specout.RouteKey, 0, len(table))
	for i := range table {
		r := &table[i]
		documented, names, err := fiberpath.OpenAPI(r.Path)
		switch {
		case err != nil:
			documented = r.Path
		case !slices.Equal(names, r.Params):
			return fmt.Errorf("specoutfiberv2: %s %s: fiber reads parameters %q, specout read %q",
				r.Method, r.Path, r.Params, names)
		}
		routes = append(routes, specout.RouteKey{Method: r.Method, Path: documented})
	}
	return d.CheckRoutes(routes, skips...)
}

// shadowed reports the routes of table that never run on app, because an
// earlier route takes their requests. A skipped route takes no part: it can
// be a guard that calls c.Next, which lets the later routes run.
func shadowed(app *fiber.App, table []fiber.Route, skips []specout.SkipRule) error {
	cfg := app.Config()
	routes := make([]fiberpath.Route, 0, len(table))
	for i := range table {
		r := fiberpath.Route{Method: table[i].Method, Path: table[i].Path}
		if !slices.ContainsFunc(skips, func(s specout.SkipRule) bool { return s.Matches(fiberpath.Table(r.Path)) }) {
			routes = append(routes, r)
		}
	}
	return fiberpath.Shadowed("specoutfiberv2", routes, fiberpath.Reading{
		CaseSensitive: cfg.CaseSensitive,
		StrictRouting: cfg.StrictRouting,
		Match:         func(path, pattern string) bool { return fiber.RoutePatternMatch(path, pattern, cfg) },
	})
}

// endpoints lists the routes of app that run endpoint handlers, in Fiber's
// match order, without the HEAD routes Fiber's Get adds. Get passes one
// handler slice to its HEAD and GET routes, so the two share it.
func endpoints(app *fiber.App) []fiber.Route {
	table := app.GetRoutes(true)
	gets := make(map[string][]*fiber.Handler)
	for i := range table {
		if r := &table[i]; r.Method == http.MethodGet && len(r.Handlers) > 0 {
			gets[r.Path] = append(gets[r.Path], &r.Handlers[0])
		}
	}
	return slices.DeleteFunc(table, func(r fiber.Route) bool {
		return r.Method == http.MethodHead && len(r.Handlers) > 0 && slices.Contains(gets[r.Path], &r.Handlers[0])
	})
}

// RequireDocumented fails the test when app holds routes specout cannot
// document.
func RequireDocumented(t specout.TestingT, d *specout.Generator, app *fiber.App, skips ...specout.SkipRule) {
	t.Helper()
	if err := Adopt(d, app, skips...); err != nil {
		t.Errorf("%v", err)
	}
}

// Record returns Fiber middleware that reports the status of every matched
// route to rec, for recorder.Verify. Fiber runs routes in registration order,
// so call app.Use(Record(rec)) before any route. Record hands a returned error
// to the app's error handler, so it reads the status the client gets.
//
// Fiber picks the endpoint only when the chain reaches it. When other
// middleware writes the response first, such as a 401, Record finds the
// endpoint the request would have reached with fiber.RoutePatternMatch.
func Record(rec *recorder.Recorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := c.Next(); err != nil {
			if herr := c.App().ErrorHandler(c, err); herr != nil {
				return fmt.Errorf("specoutfiberv2: error handler: %w", herr)
			}
		}
		route, ok := c.Locals(observedKey).(observed)
		if !ok {
			route, ok = endpointFor(c)
		}
		if ok {
			rec.Observe(route.method, route.path, c.Response().StatusCode())
		}
		return nil
	}
}

// endpointFor returns the endpoint a request matches, as the document spells
// it. A HEAD request tries the HEAD routes, then the GET routes that answer
// HEAD. The method comes from the route table: c.Method shares its bytes with
// the request, which Fiber reuses.
//
// fiber.RoutePatternMatch strips a trailing slash from the pattern only, so
// the path loses its trailing slashes here, as Fiber's router does unless
// Config.StrictRouting is set.
func endpointFor(c *fiber.Ctx) (observed, bool) {
	cfg := c.App().Config()
	methods := []string{c.Method()}
	if c.Method() == http.MethodHead {
		methods = append(methods, http.MethodGet)
	}
	p := c.Path()
	if !cfg.StrictRouting && len(p) > 1 {
		p = cmp.Or(strings.TrimRight(p, "/"), "/")
	}
	table := endpoints(c.App())
	for _, method := range methods {
		for i := range table {
			r := &table[i]
			if r.Method == method && fiber.RoutePatternMatch(p, r.Path, cfg) {
				return observed{method: r.Method, path: fiberpath.Table(r.Path)}, true
			}
		}
	}
	return observed{}, false
}
