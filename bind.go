package specout

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/happytoolin/specout/internal/bridge"
)

// bind registers rec on a router through mount, for a Binder. mount adds the
// live route: it receives the handler that the router must run and returns
// the full path that the router serves, in OpenAPI syntax.
//
// bind records rec only after mount returns, so a router that panics on a bad
// pattern leaves no record. Such a panic is raised again with the method and
// pattern in front. A second route with the same method and path panics.
// mount runs while g is locked, so it must not call g.
func (g *Generator) bind(method, pattern string, rec routeRecord, mount func(http.Handler) string) {
	checkMethod(method, pattern)
	if mount == nil {
		panic("specout: " + method + " " + pattern + ": bind needs a mount func")
	}
	rec.method, rec.pattern = method, pattern
	rec.literalStars = true
	g.addRoute(rec, func(rec *routeRecord) {
		rec.full = mountRoute(method, pattern, mount, rec)
		if err := cmp.Or(checkOpenAPIPath(rec.full), checkSegments(rec.full)); err != "" {
			panic("specout: " + method + " " + pattern + ": mounted at " + rec.full + ": " + err)
		}
		// Some routers let a later route replace an earlier one (Echo's
		// default) or never run it (Fiber), so a duplicate fails here, where
		// the stack names the second registration.
		documented := docPath(canonicalPath(rec.full))
		for _, other := range g.records {
			if other.method == method && docPath(canonicalPath(other.full)) == documented {
				panic("specout: " + method + " " + documented + ": duplicate route, registered as " +
					strconv.Quote(other.pattern) + " and " + strconv.Quote(pattern))
			}
		}
	})
}

// mountRoute runs mount and returns the path it reports. A panic from the
// router, such as Gin's on /a*b, panics again with the route named. An error
// stays reachable through errors.Is and errors.As. A message that already
// names a specout package passes through as it is.
func mountRoute(method, pattern string, mount func(http.Handler) string, h http.Handler) string {
	defer func() {
		if v := recover(); v != nil {
			panic(routePanic(method+" "+pattern, v))
		}
	}()
	return mount(h)
}

func routePanic(route string, v any) any {
	switch v := v.(type) {
	case error:
		return fmt.Errorf("specout: %s: %w", route, v)
	case string:
		if strings.HasPrefix(v, "specout") {
			return v
		}
		return "specout: " + route + ": " + v
	default:
		return fmt.Sprintf("specout: %s: %v", route, v)
	}
}

// checkRoutes reports the routes of a live router table that specout cannot
// document: a route that no registration covers, and a route with an empty
// Method, which serves every method and so is no single operation. The paths
// use OpenAPI syntax.
func (g *Generator) checkRoutes(routes []Route, skips []string) error {
	var strays []string
	for _, r := range routes {
		switch {
		case bridge.Skipped(skips, r.Path):
		case r.Method == "":
			strays = append(strays, "? "+r.Path+" (no method constraint)")
		case !g.lookup(r.Method, r.Path):
			strays = append(strays, r.Method+" "+r.Path)
		}
	}
	if len(strays) == 0 {
		return nil
	}
	// Router tables list the methods of one path in map order, and can list
	// one route twice.
	slices.Sort(strays)
	return &bridge.StrayError{Routes: slices.Compact(strays)}
}

// checkOpenAPIPath returns why p is not an absolute OpenAPI path template, or
// "" when it is one. Braces must pair up: a stray one makes every later
// parameter unreadable. Regex constraints may nest one brace pair, as in
// {id:[0-9]{4}}.
func checkOpenAPIPath(p string) string {
	if !strings.HasPrefix(p, "/") {
		return "the path must start with /"
	}
	depth := 0
	for _, c := range p {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return "the path has a } without a matching {"
			}
		}
	}
	if depth != 0 {
		return "the path has a { without a matching }"
	}
	return ""
}

// checkSegments returns why p has a segment that a request cannot carry as
// written, or "" when it has none: an empty segment (//), or a dot segment
// (/. or /..). Clients and most routers clean those from the request path,
// and chi matches them only as written, so the document would name a path
// that differs from the one served. Parameter regexes may hold any of them.
func checkSegments(p string) string {
	if msg := pathCharProblem(docPath(p)); msg != "" {
		return msg
	}
	p = strings.TrimSuffix(strings.TrimPrefix(docPath(p), "/"), "/")
	if p == "" {
		return ""
	}
	for seg := range strings.SplitSeq(p, "/") {
		switch seg {
		case "":
			return "the path has an empty segment (//); remove the extra /"
		case ".", "..":
			return "the path has a dot segment (/" + seg + "); remove it"
		}
	}
	return ""
}

// routerParamSegment reports a segment spelled in router syntax (/:id), which
// OpenAPI reads as a literal ":id" segment.
func routerParamSegment(p string) (string, bool) {
	for seg := range strings.SplitSeq(p, "/") {
		if len(seg) > 1 && seg[0] == ':' {
			return seg, true
		}
	}
	return "", false
}
