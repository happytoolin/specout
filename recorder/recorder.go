// Package recorder records the status codes that an API returns for each
// route during tests, so Verify can compare them with the document.
package recorder

import (
	"cmp"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"
	"sync"

	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/internal/hijack"
)

// Recorder records the status codes of each route, by method and path.
type Recorder struct {
	next  http.Handler
	skips []string // paths that are no API routes, such as the spec endpoint

	mu    sync.Mutex
	codes map[bridge.Key]map[int]bool
	// misses counts the requests that ServeHTTP served without finding a
	// route. Verify matches them against the paths of the document.
	misses map[miss]int
}

// miss is a request that ran no route that the recorder could see, such as a
// request that middleware outside the router answered.
type miss struct {
	method, path string
	status       int
}

// New returns a recorder that serves requests through next. For an
// http.ServeMux, it reads the pattern of the route that ran. For another
// router, it finds the route from the request path and the document. The
// adapters record through their Record middleware instead: pass a nil next
// for them. A route on a path that skips match records nothing; a skip that
// ends in /* matches every path below it.
func New(next http.Handler, skips ...string) *Recorder {
	return &Recorder{next: next, skips: skips, codes: map[bridge.Key]map[int]bool{}, misses: map[miss]int{}}
}

//nolint:gochecknoinits // the adapters reach the recorder only through this hook
func init() {
	bridge.Observe = func(rec any, method, path string, status int) {
		r, _ := rec.(*Recorder)
		if r == nil {
			panic("recorder: the recorder is nil; make one with recorder.New")
		}
		r.observe(method, path, status)
	}
}

// observe records status for one matched route. path uses OpenAPI syntax, as
// the document spells it, or router syntax that the document normalizes. A
// skipped path records nothing, and status 0 counts as 200.
func (rec *Recorder) observe(method, path string, status int) {
	if path == "" || bridge.Skipped(rec.skips, path) {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	key := bridge.NewKey(method, path)
	if rec.codes[key] == nil {
		rec.codes[key] = map[int]bool{}
	}
	rec.codes[key][cmp.Or(status, http.StatusOK)] = true
}

// ServeHTTP serves r through the wrapped router and records the status of the
// route that ran. A handler that hijacks the connection writes the response
// itself: a WebSocket handshake that wrote no status records 101, and any
// other hijack without a status records nothing.
func (rec *Recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rec.next == nil {
		panic("recorder: ServeHTTP on a Recorder without a router; route requests through the router, " +
			"whose Record middleware reports to the recorder")
	}
	hw, watch := hijack.Wrap(w)
	rec.next.ServeHTTP(hw, r)
	status, ok := watch.Result(r.Header)
	// a redirect the router made ran no route
	if !ok || routerRedirect(r, hw.Header(), status) {
		return
	}
	method, pattern := servedRoute(r)
	if pattern == "" {
		// Verify matches the path against the document
		rec.mu.Lock()
		rec.misses[miss{method: r.Method, path: r.URL.EscapedPath(), status: cmp.Or(status, http.StatusOK)}]++
		rec.mu.Unlock()
		return
	}
	if !bridge.Skipped(rec.skips, pattern) {
		rec.observe(method, bridge.MuxPath(pattern), status)
	}
}

// servedRoute returns the method and pattern of the http.ServeMux route that
// ran, or no pattern for another router. ServeMux sets r.Pattern during
// dispatch, so call servedRoute only after it.
func servedRoute(r *http.Request) (string, string) {
	method, pattern := r.Method, stdPattern(r.Pattern)
	// ServeMux dispatches HEAD to GET only when no explicit HEAD route
	// matched. Record the handler that actually ran.
	if method == http.MethodHead && strings.HasPrefix(r.Pattern, http.MethodGet+" ") {
		method = http.MethodGet
	}
	return method, pattern
}

// routerRedirect reports a redirect that a router writes in place of the
// route: http.ServeMux redirects to the cleaned path, and adds or drops a
// trailing slash. A route that redirects to its own path with the slash
// toggled reads the same, and is not recorded.
func routerRedirect(r *http.Request, h http.Header, status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return false
	}
	loc, err := url.Parse(h.Get("Location"))
	if err != nil || loc.Path == "" {
		return false
	}
	p := r.URL.Path
	return loc.Path != p && (loc.Path == cleanPath(p) || loc.Path == p+"/" || loc.Path+"/" == p)
}

// cleanPath is the path ServeMux redirects to: path.Clean, with the trailing
// slash kept.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := pathpkg.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}

// stdPattern extracts the operation path from an http.ServeMux pattern.
// Method-less subtree patterns can be outer mounts, so ignore them. A
// method-qualified subtree is a specout catch-all: it is not in the paths
// object, but its declared statuses still count.
func stdPattern(p string) string {
	if _, path, ok := strings.Cut(p, " "); ok {
		return path
	}
	if strings.HasSuffix(p, "/") {
		return ""
	}
	return p
}
