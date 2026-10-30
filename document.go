package specout

import "strings"

// Document records an already-registered handler at an absolute pattern:
// the escape hatch for a router without a binder. The handler must already
// be registered with the real router; specout only records the metadata.
// The pattern uses OpenAPI syntax: /items/{id}, not /items/:id.
func Document[Req, Res any](d *Generator, method, pattern string, h Handler[Req, Res]) {
	if d == nil {
		panic("specout: Document needs a generator")
	}
	if err := checkOpenAPIPath(pattern); err != "" {
		panic("specout: Document pattern " + pattern + ": " + err)
	}
	// /items/:id is router syntax. OpenAPI would document a literal ":id"
	// segment, and no request would ever carry it.
	if seg, ok := routerParamSegment(pattern); ok {
		panic("specout: Document pattern " + pattern + " uses router syntax " + seg +
			"; write the parameter as {" + seg[1:] + "}")
	}
	// {$} is the std mux end anchor, and // and /. are segments a request
	// cannot carry as written: each would reach the document as written,
	// unlike the path the router matches.
	if strings.Contains(pattern, "{$}") || checkSegments(pattern) != "" {
		panic("specout: Document pattern " + pattern + " is not an OpenAPI path; drop {$}, empty segments and dot segments")
	}
	checkMethod(method)
	rec := recOf(h)
	rec.method, rec.pattern = method, pattern
	rec.full = pattern
	rec.absolute = true
	d.addRoute(rec, nil)
}
