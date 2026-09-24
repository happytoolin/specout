package specout

import (
	"net/http"
	"slices"
	"strings"
)

// canonicalPath normalizes a router pattern into the path the document and the
// drift check agree on: Route groups emit "//", and the std mux spells the
// root as "/{$}". Trailing slashes stay significant, since chi walks /a and
// /a/ as distinct routes.
func canonicalPath(p string) string {
	p = strings.TrimSuffix(p, "{$}")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

// docPath rewrites router regex constraints to plain OpenAPI templates:
// chi and gorilla report /items/{id:[0-9]+}, OpenAPI only knows {id}.
func docPath(p string) string {
	return pathParamRe.ReplaceAllString(p, "{$1}")
}

// isCatchAll reports patterns OpenAPI cannot express: trailing wildcards,
// both the chi form (/files/*) and the std multi-segment form
// (/files/{path...}).
func (rec *routeRecord) isCatchAll() bool {
	p := docPath(rec.full)
	return (!rec.literalStars && strings.Contains(p, "*")) || strings.Contains(p, "...}")
}

// openAPIMethods is the path-item key set (finding 17): a token like CONNECT
// or a lowercase typo would make the emitted document invalid.
var openAPIMethods = []string{
	http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete,
	http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace,
}

// checkMethod panics on a method token OpenAPI cannot name.
func checkMethod(method string) {
	if !slices.Contains(openAPIMethods, method) {
		panic("specout: method must be one of GET/HEAD/POST/PUT/PATCH/DELETE/OPTIONS/TRACE, got " + method)
	}
}
