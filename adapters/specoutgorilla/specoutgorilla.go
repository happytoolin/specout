// Package specoutgorilla is the specout adapter for gorilla/mux. Handlers
// stay plain net/http handlers: each route copies gorilla's route variables
// into r.PathValue before the handler runs. Record feeds the recorder status
// check.
package specoutgorilla

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/internal/hijack"
	"github.com/happytoolin/specout/recorder"
)

// Router is the adapter of a gorilla router or subrouter, for specout.Bind.
// Each route is documented at its full template, subrouter prefix included. A
// regex constraint stays in the route and leaves the document: {id:[0-9]+}
// is documented as {id}. Put route middleware on a subrouter with its Use.
type Router struct{ r *mux.Router }

// New returns the adapter of r.
func New(r *mux.Router) *Router {
	if r == nil {
		panic("specoutgorilla: New needs a *mux.Router")
	}
	return &Router{r: r}
}

// Add registers h for method on p, in gorilla syntax, and returns its full
// template.
func (b *Router) Add(method, p string, h http.Handler) string {
	tpl, err := b.r.Handle(p, withVars(h)).Methods(method).GetPathTemplate()
	if err != nil {
		panic(err)
	}
	return tpl
}

// withVars copies gorilla's route variables into r.PathValue before next
// runs. gorilla keeps them in mux.Vars only.
func withVars(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, value := range mux.Vars(r) {
			r.SetPathValue(name, value)
		}
		next.ServeHTTP(w, r)
	})
}

// Routes returns the route table of the router. A route with no method
// constraint serves every method. A route with no path matcher serves every
// path and is listed as *.
func (b *Router) Routes() ([]specout.Route, error) {
	var routes []specout.Route
	err := b.r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		// A route with no handler, or with a router as its handler, is a
		// mount. Walk visits the routes of a mounted router next.
		h := route.GetHandler()
		if _, mounted := h.(*mux.Router); h == nil || mounted {
			return nil
		}
		if err := route.GetError(); err != nil {
			return fmt.Errorf("specoutgorilla: route: %w", err)
		}
		tpl, err := route.GetPathTemplate()
		if err != nil {
			tpl = "*"
		}
		// GetMethods fails for a route with no method matcher.
		methods, _ := route.GetMethods()
		if len(methods) == 0 {
			routes = append(routes, specout.Route{Path: tpl})
			return nil
		}
		for _, method := range methods {
			routes = append(routes, specout.Route{Method: method, Path: tpl})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("specoutgorilla: walk gorilla router: %w", err)
	}
	return routes, nil
}

// Record returns gorilla middleware that reports the status of every matched
// route to rec, for recorder.Verify. Register it with r.Use. gorilla runs its
// middleware after it matches a route, so a response that other middleware
// writes, such as a 401, still counts for its route. The redirect that
// StrictSlash writes in place of the route counts for no route.
//
// A handler that hijacks the connection writes the response itself: a
// WebSocket handshake that wrote no status counts as 101, and any other
// hijack without a status counts as nothing.
func Record(rec *recorder.Recorder) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hw, watch := hijack.Wrap(w)
			next.ServeHTTP(hw, r)
			route := mux.CurrentRoute(r)
			if route == nil {
				return
			}
			tpl, err := route.GetPathTemplate()
			if err != nil {
				return
			}
			status, ok := watch.Result(r.Header)
			if ok && !slashRedirect(r, hw.Header(), status) {
				bridge.Observe(rec, r.Method, tpl, status)
			}
		})
	}
}

// slashRedirect reports the 301 that gorilla's StrictSlash writes in place of
// the route: a redirect to the request path with a trailing slash added or
// dropped.
func slashRedirect(r *http.Request, h http.Header, status int) bool {
	if status != http.StatusMovedPermanently {
		return false
	}
	loc, err := url.Parse(h.Get("Location"))
	if err != nil {
		return false
	}
	p := r.URL.Path
	return loc.Path == p+"/" || loc.Path+"/" == p
}
