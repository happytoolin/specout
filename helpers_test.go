package specout_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/require"
)

// newGen is the throwaway generator every test starts from.
func newGen() *specout.Generator {
	return specout.New(specout.Config{Title: "t", Version: "1"})
}

// buildDoc renders d and returns the parsed document.
func buildDoc(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, d.WriteJSON(&buf), "build")
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc), "invalid json")
	return doc
}

// wantBuildErr fails unless building d returns an error mentioning want.
func wantBuildErr(t *testing.T, d *specout.Generator, want string) {
	t.Helper()
	require.ErrorContains(t, d.WriteJSON(&bytes.Buffer{}), want)
}

// pathsObj is the paths object of an already-built document.
func pathsObj(doc map[string]any) map[string]any {
	return doc["paths"].(map[string]any)
}

// docPaths is the document's paths object.
func docPaths(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	return pathsObj(buildDoc(t, d))
}

// opOf digs doc.paths[path][method] out of the parsed document.
func opOf(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	item, ok := doc["paths"].(map[string]any)[path].(map[string]any)
	require.True(t, ok, "no path %s in the document", path)
	op, ok := item[method].(map[string]any)
	require.True(t, ok, "no %s %s in the document", method, path)
	return op
}

// adopt runs a router binder's Adopt; fatal on error. The chi and gorilla
// binders share this method sign.
func adopt(t *testing.T, b interface {
	Adopt(skips ...specout.SkipRule) error
},
) {
	t.Helper()
	require.NoError(t, b.Adopt())
}

// serve mounts the spec on an already-adopted router and returns the document
// the router actually serves.
func serve(t *testing.T, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	r.Mount("/openapi.json", d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil))
	require.Equal(t, 200, w.Code, "openapi.json status")
	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc), "invalid json")
	return doc
}

// serveDoc adopts the chi root, mounts the spec and returns the served document.
func serveDoc(t *testing.T, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	adopt(t, specout.Chi(d, r))
	return serve(t, d, r)
}

// docOf renders routes declared with Document — no router, no walk — and
// returns the parsed document. The adapter-free workhorse.
func docOf[Req, Res any](t *testing.T, method, pattern string, h specout.Handler[Req, Res]) map[string]any {
	t.Helper()
	d := newGen()
	specout.Document(d, method, pattern, h)
	return buildDoc(t, d)
}

// chiDoc builds one document from routes declared on a fresh chi root.
func chiDoc(t *testing.T, routes func(d *specout.Generator, r chi.Router)) map[string]any {
	t.Helper()
	d, r := newGen(), chi.NewRouter()
	routes(d, r)
	return serveDoc(t, d, r)
}

func noop(http.ResponseWriter, *http.Request) {}

func okBody(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

// noBody is the metadata type of a documented route with no request fields and
// no response body: by far the most common handler a test registers.
type noBody = specout.Handler[struct{}, specout.NoContent]

// okGet is one shared noBody value. Handler identity is the func pointer, so a
// test that needs the same handler on two routes reuses this one.
var okGet = noBody{HandlerFunc: okBody}

// declaredStatuses and specStatuses are the two drift views, fatal on error.
func declaredStatuses(t *testing.T, d *specout.Generator) map[specout.RouteKey]map[int]bool {
	t.Helper()
	st, err := d.DeclaredStatuses()
	require.NoError(t, err)
	return st
}

func specStatuses(t *testing.T, d *specout.Generator) map[specout.RouteKey]map[int]bool {
	t.Helper()
	st, err := d.SpecStatuses()
	require.NoError(t, err)
	return st
}

// keys is the sorted key set of a JSON object, for stable messages.
func keys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

// schemas is the document's components.schemas object.
func schemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	s, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)
	require.True(t, ok, "no components.schemas in the document")
	return s
}

// props is one component's properties object.
func props(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	s, ok := schemas(t, doc)[name].(map[string]any)
	require.True(t, ok, "no component %s", name)
	return s["properties"].(map[string]any)
}

// isNullable reports whether a property schema admits null: a two-arm oneOf,
// or a type array ending in "null".
func isNullable(p map[string]any) bool {
	if arms, _ := p["oneOf"].([]any); len(arms) == 2 {
		return true
	}
	typ, _ := p["type"].([]any)
	return len(typ) == 2 && typ[1] == "null"
}

// paramsOf indexes an operation's parameters by name.
func paramsOf(t *testing.T, op map[string]any) map[string]map[string]any {
	t.Helper()
	raw, _ := op["parameters"].([]any)
	out := make(map[string]map[string]any, len(raw))
	for _, r := range raw {
		p := r.(map[string]any)
		out[p["name"].(string)] = p
	}
	return out
}
