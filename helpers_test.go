package specout_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

// newGen is the throwaway generator every test starts from.
func newGen() *specout.Generator {
	return specout.New(specout.Config{Title: "t", Version: "1"})
}

// buildDoc renders d and returns the parsed document.
func buildDoc(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatalf("build: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	return doc
}

// wantErr fails unless err mentions want.
func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error containing %q, got %v", want, err)
	}
}

// wantBuildErr fails unless building d returns an error mentioning want.
func wantBuildErr(t *testing.T, d *specout.Generator, want string) {
	t.Helper()
	wantErr(t, d.WriteJSON(&bytes.Buffer{}), want)
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
	if !ok {
		t.Fatalf("no path %s in the document: %v", path, doc["paths"])
	}
	op, ok := item[method].(map[string]any)
	if !ok {
		t.Fatalf("no %s %s in the document", method, path)
	}
	return op
}

// adopt runs a router binder's Adopt; fatal on error. The chi and gorilla
// binders share this method sign.
func adopt(t *testing.T, b interface {
	Adopt(...specout.SkipRule) error
}) {
	t.Helper()
	if err := b.Adopt(); err != nil {
		t.Fatal(err)
	}
}

// serve mounts the spec on an already-adopted router and returns the document
// the router actually serves.
func serve(t *testing.T, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	r.Mount("/openapi.json", d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != 200 {
		t.Fatalf("openapi.json status %d", w.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	return doc
}

// serveDoc adopts the chi root, mounts the spec and returns the served document.
func serveDoc(t *testing.T, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	adopt(t, specout.Chi(d, r))
	return serve(t, d, r)
}

// getDoc registers one GET route on a fresh chi root and returns the served
// document. It is the single-route workhorse; a test that needs a second route
// or another verb builds its own router and calls serveDoc.
func getDoc[Req, Res any](t *testing.T, pattern string, h specout.Handler[Req, Res]) map[string]any {
	t.Helper()
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get(pattern, h)
	return serveDoc(t, d, r)
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

// noGet is a noBody handler on its own func value, distinct from okGet: a test
// that needs two different handlers on one method uses one of each.
var noGet = noBody{HandlerFunc: noop}

func noop(http.ResponseWriter, *http.Request) {}

func okBody(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }

// noBody is the metadata type of a documented route with no request fields and
// no response body: by far the most common handler a test registers.
type noBody = specout.Handler[struct{}, specout.NoContent]

// okGet is one shared noBody value. Handler identity is the func pointer, so a
// test that needs the same handler on two routes reuses this one.
var okGet = noBody{HandlerFunc: okBody}

// panics fails unless fn panics.
func panics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("want a panic")
		}
	}()
	fn()
}

// declaredStatuses and specStatuses are the two drift views, fatal on error.
func declaredStatuses(t *testing.T, d *specout.Generator) map[specout.RouteKey]map[int]bool {
	t.Helper()
	st, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func specStatuses(t *testing.T, d *specout.Generator) map[specout.RouteKey]map[int]bool {
	t.Helper()
	st, err := d.SpecStatuses()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// keys is the sorted key set of a JSON object, for stable messages.
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// schemas is the document's components.schemas object.
func schemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	s, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)
	if !ok {
		t.Fatal("no components.schemas in the document")
	}
	return s
}

// props is one component's properties object.
func props(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	s, ok := schemas(t, doc)[name].(map[string]any)
	if !ok {
		t.Fatalf("no component %s", name)
	}
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
