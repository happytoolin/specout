package specout

import (
	"net/http"
	"reflect"
	"strings"
)

// Handle registers h on the std mux and records its metadata. Std patterns
// are full by construction ("DELETE /onboarding/{id}"), so no walk is needed.
func (d *Generator) Handle[Req, Res any](mux *http.ServeMux, pattern string, h Handler[Req, Res]) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		panic("specout: std pattern must be 'METHOD /path'")
	}
	mux.HandleFunc(pattern, h.HandlerFunc)
	d.register(routeRecord{
		method: method, pattern: path, full: path,
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary,
		deprecated: h.Deprecated, fn: h.HandlerFunc,
	})
}
