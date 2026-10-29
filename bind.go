package specout

import (
	"net/http"
	"strconv"
	"strings"
)

// Bind registers h on a router that has no binder in this package. The Echo,
// Gin and Fiber adapter packages build on it, and so can a binder for any
// other router. mount adds the live route: it receives the
// handler the router must run and returns the full path the router serves,
// in OpenAPI syntax: {name} for a parameter, {name...} for a catch-all.
//
// Bind checks the method and the freeze before mount runs, and records h only
// after mount returns, so a router that panics on a bad pattern leaves no
// record. A second route with the same method and path panics. mount runs
// while d is locked, so it must not call d. The route's identity is its method
// and returned path; CheckRoutes compares a live route table against it.
func Bind[Req, Res any](d *Generator, method, pattern string, h Handler[Req, Res], mount func(http.Handler) string) {
	checkMethod(method)
	if mount == nil {
		panic("specout: Bind needs a mount func for " + method + " " + pattern)
	}
	rec := recOf(h)
	rec.method, rec.pattern = method, pattern
	rec.absolute, rec.literalStars, rec.byPath = true, true, true
	d.addRoute(rec, func(rec *routeRecord) {
		rec.full = mount(rec)
		if err := checkOpenAPIPath(rec.full); err != "" {
			panic("specout: " + method + " " + pattern + " mounted at " + rec.full + ": " + err)
		}
		// Some routers let a later route replace an earlier one (Echo's
		// default) or never run it (Fiber), so a duplicate fails here, where
		// the stack names the second registration.
		documented := docPath(canonicalPath(rec.full))
		for _, other := range d.records {
			if other.absolute && other.method == method && docPath(canonicalPath(other.full)) == documented {
				panic("specout: duplicate route " + method + " " + documented + " (registered as " +
					strconv.Quote(other.pattern) + " and " + strconv.Quote(pattern) + ")")
			}
		}
	})
}

// CheckRoutes reports the routes of a live router table that specout cannot
// document: a route that no Bind or Document call covers, and a route with an
// empty Method, which serves every method and so is no single operation.
// Paths use OpenAPI syntax, as Bind returns them. The error is a *StrayError.
// Router binders built on Bind call it from their Adopt.
func (d *Generator) CheckRoutes(routes []RouteKey, skips ...SkipRule) error {
	s := strayScan{d: d, skips: skips}
	for _, r := range routes {
		s.collect(r.Path, r.Method, nil)
	}
	return s.err()
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
