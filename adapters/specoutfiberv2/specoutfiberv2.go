// Package specoutfiberv2 binds specout handlers to Fiber v2. Fiber does not
// run net/http handlers natively, so each route runs its handler through
// Fiber's adaptor, which builds an *http.Request for every call; Fiber's path
// parameters arrive as r.PathValue. Adopt checks Fiber's route table for
// routes that carry no specout metadata, and Record feeds the recorder status
// check. For Fiber v3, use specoutfiber.
package specoutfiberv2

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/happytoolin/specout"
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
func New(d *specout.Generator, r fiber.Router) *Router {
	if d == nil {
		panic("specoutfiberv2: New needs a generator")
	}
	switch g := r.(type) {
	case *fiber.App:
		return &Router{d: d, r: g}
	case *fiber.Group:
		return &Router{d: d, r: g, prefix: g.Prefix}
	default:
		panic(fmt.Sprintf("specoutfiberv2: New needs a *fiber.App or *fiber.Group, got %T", r))
	}
}

// Handle registers h for method on p. Route middleware m runs before h. A GET
// route is registered with Fiber's Get, so it answers HEAD too.
func (b *Router) Handle[Req, Res any](method, p string, h specout.Handler[Req, Res], m ...fiber.Handler) {
	full := groupPath(b.prefix, p)
	documented, err := openAPIPath(full)
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
		chain := append(slices.Clip(m), serve(next, observed{method: method, path: documented}))
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
// stores it in the request locals for Record.
type observed struct{ method, path string }

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
// locals, so path parameters cross over as a local. They are copied: Fiber
// reuses the bytes of its strings after the request, and a net/http handler
// may keep r.PathValue results.
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
		params := c.Route().Params
		values := make([]pathValue, 0, len(params))
		for _, name := range params {
			values = append(values, pathValue{name: name, value: strings.Clone(c.Params(name))})
		}
		c.Locals(pathValuesKey, values)
		c.Locals(observedKey, route)
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
// Fiber parsed from it, so syntax specout reads differently fails here. The
// routes of a mounted app join app's table, at the mount path, when app
// starts; a Router on the mounted app documents them without that path.
func Adopt(d *specout.Generator, app *fiber.App, skips ...specout.SkipRule) error {
	table := endpoints(app)
	routes := make([]specout.RouteKey, 0, len(table))
	for i := range table {
		r := &table[i]
		documented, names, err := parsePath(r.Path)
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
				return observed{method: r.Method, path: tablePath(r.Path)}, true
			}
		}
	}
	return observed{}, false
}

// tablePath translates a path from Fiber's route table. A path Handle could
// not have registered stays as Fiber spells it, so a report still names it.
func tablePath(p string) string {
	full, _, err := parsePath(p)
	if err != nil {
		return p
	}
	return full
}

// groupPath is Fiber's own join of a group prefix and a route path, with the
// leading slash Fiber adds to a bare path. An empty path keeps the prefix as
// written, trailing slash included.
func groupPath(prefix, p string) string {
	full := prefix
	if p != "" {
		if p[0] != '/' {
			p = "/" + p
		}
		full = strings.TrimRight(prefix, "/") + p
	}
	if full == "" || full[0] != '/' {
		full = "/" + full
	}
	return full
}

// openAPIPath translates a Fiber route path into OpenAPI syntax.
func openAPIPath(p string) (string, error) {
	full, _, err := parsePath(p)
	return full, err
}

// parsePath translates a Fiber route path into OpenAPI syntax and lists the
// parameter names in Fiber's order: /users/:id becomes /users/{id}, and
// /:id<int> drops its constraint, since the Req field types the parameter. A
// wildcard * or + becomes a catch-all such as {*1...}; Fiber names them *1,
// *2 and +1, +2 in order. A backslash makes the next character literal, and
// so does a doubled colon.
//
// An optional parameter (:id?) matches two paths, and OpenAPI has no optional
// path parameter, so it is an error: register both paths instead. Any other
// syntax this scan does not know is an error too, rather than a guess.
func parsePath(p string) (string, []string, error) {
	var b strings.Builder
	var names []string
	wildcards, pluses := 0, 0
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '\\':
			if i+1 == len(p) {
				return "", nil, fmt.Errorf("trailing backslash in %q", p)
			}
			i++
			if p[i] == '{' || p[i] == '}' {
				return "", nil, fmt.Errorf("literal braces in %q read as OpenAPI parameters", p)
			}
			b.WriteByte(p[i])
		case ':':
			// :: is a literal colon before a parameter: color::color
			// matches color:blue.
			if i+1 < len(p) && p[i+1] == ':' {
				b.WriteByte(':')
				continue
			}
			name, end, err := param(p, i+1)
			if err != nil {
				return "", nil, err
			}
			names = append(names, name)
			b.WriteString("{" + name + "}")
			i = end - 1
		case '*', '+':
			n := &wildcards
			if c == '+' {
				n = &pluses
			}
			*n++
			name := string(c) + strconv.Itoa(*n)
			names = append(names, name)
			b.WriteString("{" + name + "...}")
		case '{', '}':
			return "", nil, fmt.Errorf("literal braces in %q read as OpenAPI parameters", p)
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), names, nil
}

// param reads the parameter name that starts at start, with its optional
// <constraint>, and returns the index after it. A name ends at a slash, a
// dash, a dot, another parameter or an escape, as in Fiber's parser.
func param(p string, start int) (string, int, error) {
	i := start
	for i < len(p) && !strings.ContainsRune(`/-.:\?<*+{}`, rune(p[i])) {
		i++
	}
	name := p[start:i]
	if name == "" {
		return "", 0, fmt.Errorf("empty parameter name in %q", p)
	}
	if i < len(p) && p[i] == '<' {
		end := constraintEnd(p, i+1)
		if end < 0 {
			return "", 0, fmt.Errorf("unclosed constraint on :%s in %q", name, p)
		}
		i = end + 1
	}
	if i < len(p) {
		switch p[i] {
		case '?':
			return "", 0, fmt.Errorf("optional parameter :%s? matches two paths; register each path", name)
		case '*', '+', '{', '}':
			return "", 0, fmt.Errorf("parameter :%s is followed by %q, which specout cannot read", name, p[i])
		}
	}
	return name, i, nil
}

// constraintEnd returns the index of the > that closes a constraint opened
// before start, skipping escaped characters, or -1.
func constraintEnd(p string, start int) int {
	for i := start; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '>':
			return i
		}
	}
	return -1
}
