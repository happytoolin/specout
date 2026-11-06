// Package specoutgin is the specout adapter for Gin. Handlers stay plain
// net/http handlers: each route copies Gin's path parameters into
// r.PathValue before the handler runs. Record feeds the recorder status
// check.
package specoutgin

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
)

// Routes is what a Router registers through: *gin.Engine and *gin.RouterGroup.
type Routes interface {
	gin.IRoutes
	BasePath() string
}

// Router is the adapter of a Gin engine or group, for specout.Bind. Each
// route is documented at its full path, group prefix included. Put route
// middleware on a group: engine.Group("/admin", auth).
type Router struct{ r Routes }

// New returns the adapter of r.
func New(r Routes) *Router {
	if r == nil {
		panic("specoutgin: New needs a *gin.Engine or *gin.RouterGroup")
	}
	return &Router{r: r}
}

// Add registers h for method on p, in Gin syntax, and returns its full path in
// OpenAPI syntax.
func (b *Router) Add(method, p string, h http.Handler) string {
	full := joinPaths(b.r.BasePath(), p)
	documented, err := openAPIPath(full)
	if err != nil {
		panic(fmt.Sprintf("specoutgin: %s %s: %v", method, full, err))
	}
	b.r.Handle(method, p, serve(h, observed{method: method, path: documented}))
	return documented
}

// Routes returns the route table of the engine. Gin's Any registers one route
// per method, so its CONNECT route is listed too. A group lists no routes, so
// pass Check the adapter of the engine.
func (b *Router) Routes() ([]specout.Route, error) {
	e, ok := b.r.(*gin.Engine)
	if !ok {
		return nil, errors.New("specoutgin: a group lists no routes; pass Check specoutgin.New(engine)")
	}
	table := e.Routes()
	routes := make([]specout.Route, 0, len(table))
	for _, ri := range table {
		routes = append(routes, specout.Route{Method: ri.Method, Path: tablePath(ri.Path)})
	}
	return routes, nil
}

// observed is the route a request ran, as the document spells it. serve
// stores it on the Gin context for Record.
type observed struct{ method, path string }

type observedKey struct{}

// serve runs next as the route's last handler. Gin keeps path parameters on
// its own context, so they are copied onto the request first, as Gin gives
// them.
func serve(next http.Handler, route observed) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(observedKey{}, route)
		for _, p := range c.Params {
			c.Request.SetPathValue(p.Key, p.Value)
		}
		next.ServeHTTP(c.Writer, c.Request)
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
			bridge.Observe(rec, method, p, status)
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
		return c.Request.Method, tablePath(full), true
	}
	return "", "", false
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
// becomes /users/{id}, and a trailing *path becomes the catch-all {path...}.
// A parameter name runs to the next slash, as in Gin's findWildcard.
func openAPIPath(p string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '\\':
			return "", fmt.Errorf("escaped characters in %q have no OpenAPI form", p)
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
