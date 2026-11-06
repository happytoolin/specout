package specout

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// canonicalPath normalizes a router pattern into the path the document and the
// drift check agree on: a joined prefix can leave "//", and http.ServeMux
// ends a path that serves itself only with "{$}". Trailing slashes stay
// significant: /a and /a/ are two routes.
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

// isCatchAll reports patterns OpenAPI cannot express: the multi-segment form
// {path...}, and a * in a Document path.
func (rec *routeRecord) isCatchAll() bool {
	p := docPath(rec.full)
	return (!rec.literalStars && strings.Contains(p, "*")) || strings.Contains(p, "...}")
}

// openAPIMethods are the methods that an OpenAPI path item can hold.
var openAPIMethods = []string{
	http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete,
	http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace,
}

func checkMethod(method, where string) {
	if !slices.Contains(openAPIMethods, method) {
		panic("specout: " + where + ": method " + strconv.Quote(method) +
			" is not valid; use GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS or TRACE" + didYouMean(method, openAPIMethods))
	}
}
