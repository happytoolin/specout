// Package specoutgin binds specout handlers to Gin. Handlers stay plain
// net/http handlers: each route copies Gin's path parameters into
// r.PathValue before the handler runs. Adopt checks Gin's route table for
// routes that carry no specout metadata, and Record feeds the recorder status
// check.
package specoutgin

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
)

// Routes is what a Router registers through: *gin.Engine and *gin.RouterGroup.
type Routes interface {
	gin.IRoutes
	BasePath() string
}

// Router registers specout handlers on a Gin engine or group. Each route is
// documented at its full path, group prefix included.
type Router struct {
	d *specout.Generator
	r Routes
}

// New returns the Gin binder for d on r.
func New(d *specout.Generator, r Routes) *Router {
	if d == nil || r == nil {
		panic("specoutgin: New needs a generator and a *gin.Engine or *gin.RouterGroup")
	}
	return &Router{d: d, r: r}
}

// Handle registers h for method on p. Route middleware m runs before h.
func (b *Router) Handle[Req, Res any](method, p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	full := joinPaths(b.r.BasePath(), p)
	documented, err := openAPIPath(full)
	if err != nil {
		panic(fmt.Sprintf("specoutgin: %s %s: %v", method, full, err))
	}
	// Bind checks the route after Gin adds it, and Gin cannot remove a route.
	// So the route answers 404 until Bind returns: a registration that panics
	// serves nothing, even when the caller recovers.
	var bound atomic.Bool
	specout.Bind(b.d, method, p, h, func(next http.Handler) string {
		run := gate(&bound, serve(next, observed{method: method, path: documented}))
		b.r.Handle(method, p, append(slices.Clip(m), run)...)
		return documented
	})
	remember(method, full, documented)
	bound.Store(true)
}

// gate returns h, which answers 404 until bound is set.
func gate(bound *atomic.Bool, h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !bound.Load() {
			http.NotFound(c.Writer, c.Request)
			return
		}
		h(c)
	}
}

// Get registers a GET route on p.
func (b *Router) Get[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodGet, p, h, m...)
}

// Head registers a HEAD route on p.
func (b *Router) Head[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodHead, p, h, m...)
}

// Post registers a POST route on p.
func (b *Router) Post[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodPost, p, h, m...)
}

// Put registers a PUT route on p.
func (b *Router) Put[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodPut, p, h, m...)
}

// Patch registers a PATCH route on p.
func (b *Router) Patch[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodPatch, p, h, m...)
}

// Delete registers a DELETE route on p.
func (b *Router) Delete[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodDelete, p, h, m...)
}

// Options registers an OPTIONS route on p.
func (b *Router) Options[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodOptions, p, h, m...)
}

// Trace registers a TRACE route on p.
func (b *Router) Trace[Req, Res any](p string, h specout.Handler[Req, Res], m ...gin.HandlerFunc) {
	b.Handle(http.MethodTrace, p, h, m...)
}

// observed is the route a request ran, as the document spells it. serve
// stores it on the Gin context for Record.
type observed struct{ method, path string }

// observedKey types the Gin context key serve stores observed under.
type observedKey struct{}

// serve runs next as the route's last handler. Gin keeps path parameters on
// its own context, so they are copied onto the request first.
func serve(next http.Handler, route observed) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(observedKey{}, route)
		for _, p := range c.Params {
			c.Request.SetPathValue(p.Key, p.Value)
		}
		next.ServeHTTP(c.Writer, c.Request)
	}
}

// Adopt reports the routes of e that specout cannot document: routes no
// Router registered. Gin's Any registers one route per method, so its
// CONNECT route is reported too. Skip the spec endpoint itself, as in
// specout.Skip("/openapi.json").
func Adopt(d *specout.Generator, e *gin.Engine, skips ...specout.SkipRule) error {
	table := e.Routes()
	routes := make([]specout.RouteKey, 0, len(table))
	for _, ri := range table {
		routes = append(routes, tableKey(d, ri.Method, ri.Path))
	}
	return d.CheckRoutes(routes, skips...)
}

// maxEscapeReadings caps the colons tableKey tries to read as escaped: each
// one doubles the readings.
const maxEscapeReadings = 8

// tableKey returns the route key of one path in Gin's route table. Gin turns
// \: into : in its tree on the first request, so after it a colon in
// /items/:id/x:y may be the literal of /items/:id/x\:y. When the plain reading
// is not documented, tableKey takes the first reading with some colons
// escaped that is.
func tableKey(d *specout.Generator, method, p string) specout.RouteKey {
	key := specout.RouteKey{Method: method, Path: tablePath(p)}
	colons := colonIndexes(p)
	if len(colons) == 0 || len(colons) > maxEscapeReadings || strings.Contains(p, `\:`) || documented(d, key) {
		return key
	}
	for mask := 1; mask < 1<<len(colons); mask++ {
		literal := specout.RouteKey{Method: method, Path: tablePath(escapeColons(p, colons, mask))}
		if documented(d, literal) {
			return literal
		}
	}
	return key
}

// documented reports whether d documents key.
func documented(d *specout.Generator, key specout.RouteKey) bool {
	return d.CheckRoutes([]specout.RouteKey{key}) == nil
}

// colonIndexes returns the index of each colon in p.
func colonIndexes(p string) []int {
	var colons []int
	for i := range len(p) {
		if p[i] == ':' {
			colons = append(colons, i)
		}
	}
	return colons
}

// escapeColons puts a backslash before each colon of p whose bit is set in
// mask.
func escapeColons(p string, colons []int, mask int) string {
	var b strings.Builder
	last := 0
	for bit, i := range colons {
		if mask&(1<<bit) != 0 {
			b.WriteString(p[last:i] + `\`)
			last = i
		}
	}
	b.WriteString(p[last:])
	return b.String()
}

// RequireDocumented fails the test when e holds routes specout cannot
// document.
func RequireDocumented(t specout.TestingT, d *specout.Generator, e *gin.Engine, skips ...specout.SkipRule) {
	t.Helper()
	if err := Adopt(d, e, skips...); err != nil {
		t.Errorf("%v", err)
	}
}

// Record returns Gin middleware that reports the status of every matched
// route to rec, for recorder.Verify. Gin copies middleware into a route when
// the route is registered, so call e.Use(Record(rec)) before any route. Gin
// routes before it runs middleware, so a response that other middleware
// writes, such as a 401, still counts for its route.
//
// A handler that hijacks the connection writes the response itself: a
// WebSocket handshake that wrote no status counts as 101, and any other
// hijack without a status counts as nothing.
func Record(rec *recorder.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		watch := &hijackWatch{ResponseWriter: c.Writer}
		c.Writer = watch
		c.Next()
		c.Writer = watch.ResponseWriter
		status, ok := hijack.Status(c.Request.Header, watch.Status(), watch.wrote, watch.watch.Hijacked())
		if !ok {
			return
		}
		if method, p, routed := ranRoute(c); routed {
			rec.Observe(method, p, status)
		}
	}
}

// ranRoute returns the method and path of the route that c ran, as the
// document spells them. A specout route names itself. When middleware wrote
// the response before the handler, Gin's full path names the route instead.
func ranRoute(c *gin.Context) (string, string, bool) {
	v, _ := c.Get(observedKey{})
	if route, ok := v.(observed); ok {
		return route.method, route.path, true
	}
	if full := c.FullPath(); full != "" {
		return c.Request.Method, documentedPath(c.Request.Method, full), true
	}
	return "", "", false
}

// routeKey is the method and Gin path of a route.
type routeKey struct{ method, path string }

// documentedPaths holds the documented path of each specout route, under
// its routeKey, for the life of the process. Gin drops the \: escapes from
// c.FullPath after the first request, so a route is stored under both
// spellings. A spelling that two routes on two engines share maps to "".
var documentedPaths sync.Map

// remember stores documented, the documented path of the specout route at
// Gin path full.
func remember(method, full, documented string) {
	for _, p := range []string{full, strings.ReplaceAll(full, `\:`, ":")} {
		key := routeKey{method, p}
		if v, loaded := documentedPaths.LoadOrStore(key, documented); loaded && v != documented {
			documentedPaths.Store(key, "")
		}
	}
}

// documentedPath returns the documented path of the route at Gin path full.
// A route that no Router registered keeps Gin's spelling, translated.
func documentedPath(method, full string) string {
	v, _ := documentedPaths.Load(routeKey{method, full})
	if p, ok := v.(string); ok && p != "" {
		return p
	}
	return tablePath(full)
}

// hijackWatch notes a Hijack of the connection, and whether Gin had written a
// status before it: Gin reports 200 for a response that wrote none.
type hijackWatch struct {
	gin.ResponseWriter

	watch hijack.Watch
	wrote bool
}

func (w *hijackWatch) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.wrote = w.Written()
	return w.watch.Hijack(w.ResponseWriter)
}

// Unwrap returns Gin's writer, for http.ResponseController.
func (w *hijackWatch) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// segmentEnd returns the index of the first slash at or after i, or len(p).
func segmentEnd(p string, i int) int {
	if n := strings.IndexByte(p[i:], '/'); n >= 0 {
		return i + n
	}
	return len(p)
}

// joinPaths is Gin's own join of a group base path and a route path: clean
// the result, but keep a trailing slash the route asked for.
func joinPaths(base, rel string) string {
	if rel == "" {
		return base
	}
	full := path.Join(base, rel)
	if strings.HasSuffix(rel, "/") && !strings.HasSuffix(full, "/") {
		return full + "/"
	}
	return full
}

// tablePath translates a path from Gin's route table. A path Handle could not
// have registered stays as Gin spells it, so a report still names it.
func tablePath(p string) string {
	full, err := openAPIPath(p)
	if err != nil {
		return p
	}
	return full
}

// openAPIPath translates a Gin route path into OpenAPI syntax: /users/:id
// becomes /users/{id}, a trailing *path becomes the catch-all {path...}, and
// an escaped \: is a literal colon. A parameter name runs to the next slash,
// as in Gin's findWildcard.
func openAPIPath(p string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '\\':
			if i+1 == len(p) || p[i+1] != ':' {
				return "", fmt.Errorf("gin escapes only a colon, as in \\:, in %q", p)
			}
			b.WriteByte(':')
			i++
		case ':', '*':
			name, end, err := param(p, i)
			if err != nil {
				return "", err
			}
			b.WriteString("{" + name + "}")
			i = end - 1
		case '{', '}':
			return "", fmt.Errorf("literal braces in %q read as OpenAPI parameters", p)
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// param reads the parameter or catch-all that starts at i and returns its
// OpenAPI name and the index after it. The name runs to the next slash, and a
// catch-all must end the path.
func param(p string, i int) (string, int, error) {
	end := segmentEnd(p, i)
	name := p[i+1 : end]
	if name == "" || strings.ContainsAny(name, `{}:*\`) {
		return "", 0, fmt.Errorf("parameter name %q cannot be an OpenAPI parameter", name)
	}
	if p[i] != '*' {
		return name, end, nil
	}
	if end != len(p) {
		return "", 0, fmt.Errorf("catch-all *%s must end the path", name)
	}
	return name + "...", end, nil
}
