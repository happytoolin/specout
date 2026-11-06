// Package specoutfiber is the specout adapter for Fiber v3. Fiber does not run
// net/http handlers natively, so each route runs its handler through Fiber's
// adaptor, which builds an *http.Request for every call; Fiber's path
// parameters arrive as r.PathValue. Record feeds the recorder status check.
package specoutfiber

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutfiber/internal/fiberpath"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
)

// Router is the adapter of a Fiber app or group, for specout.Bind. Each route
// is documented at its full path, group prefix included. Put route middleware
// on a group: app.Group("/admin", auth).
//
// Fiber also answers HEAD on a GET route unless
// Config.DisableHeadAutoRegister is set; like http.ServeMux, the document
// lists the GET operation only.
type Router struct {
	r      fiber.Router
	prefix string
}

// New returns the adapter of r, a *fiber.App or *fiber.Group. A domain router
// hides its host from the path, so New rejects it.
//
// Use the app that serves requests, or a group of it. Fiber adds the mount
// prefix of a sub-app to its routes only when the app starts, so a Router on
// a sub-app documents its paths without that prefix. Record then reports the
// paths that ran, and recorder.Verify fails.
func New(r fiber.Router) *Router {
	switch g := r.(type) {
	case *fiber.App:
		if g != nil {
			return &Router{r: g}
		}
	case *fiber.Group:
		if g != nil {
			return &Router{r: g, prefix: g.Prefix}
		}
	}
	panic(fmt.Sprintf("specoutfiber: New needs a non-nil *fiber.App or *fiber.Group, got %T", r))
}

// Add registers h for method on p, in Fiber syntax, and returns its full path
// in OpenAPI syntax.
func (b *Router) Add(method, p string, h http.Handler) string {
	full := fiberpath.Join(b.prefix, p)
	documented, _, err := fiberpath.OpenAPI(full)
	if err != nil {
		panic(fmt.Sprintf("specoutfiber: %s %s: %v", method, full, err))
	}
	b.r.Add([]string{method}, p, serve(h, observed{method: method, path: documented, pattern: full}))
	return documented
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

type pathValue struct{ name, value string }

type localKey int

const (
	pathValuesKey localKey = iota
	observedKey
)

// serve runs next as the route's last handler. The adaptor builds the
// *http.Request on Fiber's request context, so path parameters cross over as
// a request local. They arrive as Fiber gives them, but copied: Fiber may
// reuse the bytes of its strings after the request, and a net/http handler
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
	return func(c fiber.Ctx) error {
		params := c.Route().Params
		values := make([]pathValue, 0, len(params))
		for _, name := range params {
			values = append(values, pathValue{name: name, value: strings.Clone(c.Params(name))})
		}
		c.Locals(pathValuesKey, values)
		c.Locals(observedKey, route.ran(c.Route().Path))
		return h(c)
	}
}

// Routes returns the route table of the app. Middleware routes are not
// endpoints and are left out. So are the HEAD routes Fiber adds for every GET
// route when it starts: the GET route answers for them. A HEAD route
// registered on its own is listed. A group lists no routes, so pass Check the
// adapter of the app.
//
// Routes also checks the translation of every path against the parameters
// Fiber parsed from it, so syntax specout reads differently fails here.
func (b *Router) Routes() ([]specout.Route, error) {
	app, ok := b.r.(*fiber.App)
	if !ok {
		return nil, errors.New("specoutfiber: a group lists no routes; pass Check specoutfiber.New(app)")
	}
	table := endpoints(app)
	routes := make([]specout.Route, 0, len(table))
	for i := range table {
		r := &table[i]
		documented, names, err := fiberpath.OpenAPI(r.Path)
		switch {
		case err != nil:
			documented = r.Path
		case !slices.Equal(names, r.Params):
			return nil, fmt.Errorf("specoutfiber: %s %s: fiber reads parameters %q, specout read %q",
				r.Method, r.Path, r.Params, names)
		}
		routes = append(routes, specout.Route{Method: r.Method, Path: documented})
	}
	return routes, nil
}

// endpoints lists the routes of app that run endpoint handlers, without the
// HEAD routes Fiber adds for GET routes. Fiber marks
// those in an unexported field, but builds each one as a copy of its GET
// route, so the two share one handler slice.
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

// Record returns Fiber middleware that reports the status of every matched
// route to rec, for recorder.Verify. Fiber runs routes in registration order,
// so call app.Use(Record(rec)) before any route. Record hands a returned error
// to the app's error handler, so it reads the status the client gets.
//
// Fiber picks the endpoint only when the chain reaches it. When other
// middleware writes the response first, such as a 401, Record finds the
// endpoint the request would have reached with fiber.RoutePatternMatch.
//
// A net/http handler that hijacks the connection writes the response itself:
// a WebSocket handshake counts as 101, and any other hijack counts as nothing.
func Record(rec *recorder.Recorder) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := c.Next(); err != nil {
			if herr := c.App().ErrorHandler(c, err); herr != nil {
				return fmt.Errorf("specoutfiber: error handler: %w", herr)
			}
		}
		route, ok := c.Locals(observedKey).(observed)
		if !ok {
			route, ok = endpointFor(c)
		}
		if !ok {
			return nil
		}
		if status, recorded := recordedStatus(c); recorded {
			bridge.Observe(rec, route.method, route.path, status)
		}
		return nil
	}
}

// recordedStatus returns the status to record for c, and false when there is
// none. A net/http handler that hijacked the connection wrote the response
// itself, and Fiber kept its default 200: a WebSocket handshake counts as
// 101, and any other hijack as nothing. A flushed response streams its body,
// so its status reached the client before any hijack.
func recordedStatus(c fiber.Ctx) (int, bool) {
	res := c.Response()
	if res.IsBodyStream() || !c.RequestCtx().Hijacked() {
		return res.StatusCode(), true
	}
	return hijack.Status(http.Header(c.GetReqHeaders()), 0, false, true)
}

// endpointFor returns the endpoint a request matches, as the document spells
// it. A HEAD request tries the HEAD routes, then the GET routes that Fiber
// copies for HEAD.
func endpointFor(c fiber.Ctx) (observed, bool) {
	cfg := c.App().Config()
	methods := []string{c.Method()}
	if c.Method() == http.MethodHead {
		methods = append(methods, http.MethodGet)
	}
	table := endpoints(c.App())
	for _, method := range methods {
		for i := range table {
			r := &table[i]
			if r.Method == method && fiber.RoutePatternMatch(c.Path(), r.Path, cfg) {
				return observed{method: r.Method, path: fiberpath.Table(r.Path)}, true
			}
		}
	}
	return observed{}, false
}
