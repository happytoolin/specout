package specout_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/stretchr/testify/require"
)

func newGen() *specout.Generator {
	return specout.New(specout.Config{Title: "t", Version: "1"})
}

func buildDoc(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	spec, err := d.Build()
	require.NoError(t, err, "build")
	exportValidationDoc(t, spec)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(spec, &doc), "invalid json")
	return doc
}

func wantBuildErr(t *testing.T, d *specout.Generator, want string) {
	t.Helper()
	_, err := d.Build()
	require.ErrorContains(t, err, want)
}

func pathsObj(doc map[string]any) map[string]any {
	return doc["paths"].(map[string]any)
}

func docPaths(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	return pathsObj(buildDoc(t, d))
}

func opOf(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	item, ok := doc["paths"].(map[string]any)[path].(map[string]any)
	require.True(t, ok, "no path %s in the document", path)
	op, ok := item[method].(map[string]any)
	require.True(t, ok, "no %s %s in the document", method, path)
	return op
}

func exportValidationDoc(t *testing.T, document []byte) {
	t.Helper()
	dir := os.Getenv("SPECOUT_VALIDATE_DIR")
	if dir == "" {
		return
	}
	f, err := os.CreateTemp(dir, strings.ReplaceAll(t.Name(), "/", "_")+"-*.json")
	require.NoError(t, err)
	_, err = f.Write(document)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func docOf[Req, Res any](t *testing.T, method, pattern string, h specout.Handler[Req, Res]) map[string]any {
	t.Helper()
	d := newGen()
	d.Document(method, pattern, h)
	return buildDoc(t, d)
}

func stdDoc(t *testing.T, routes func(r *specout.Binder)) map[string]any {
	t.Helper()
	d, mux := newGen(), http.NewServeMux()
	routes(specout.Bind(d, specoutstd.New(mux)))
	return buildDoc(t, d)
}

// fakeRouter is a specout.Router whose Add runs mount and whose Routes
// returns routes.
type fakeRouter struct {
	mount  func(http.Handler) string
	routes []specout.Route
}

func (f fakeRouter) Add(_, _ string, h http.Handler) string { return f.mount(h) }

func (f fakeRouter) Routes() ([]specout.Route, error) { return f.routes, nil }

// bind registers h for method on p through a router whose Add runs mount.
func bind[Req, Res any](d *specout.Generator, method, p string, h specout.Handler[Req, Res], mount func(http.Handler) string) {
	specout.Bind(d, fakeRouter{mount: mount}).Handle(method, p, h)
}

func noop(http.ResponseWriter, *http.Request) {}

func okBody(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

type noBody = specout.Handler[struct{}, specout.NoContent]

var okGet = noBody{HandlerFunc: okBody}

func okWith(responses ...specout.Response) noBody {
	return noBody{HandlerFunc: okBody, Responses: responses}
}

func declaredStatuses(t *testing.T, d *specout.Generator) map[bridge.Key]map[int]bool {
	t.Helper()
	st, err := bridge.Statuses(d, false)
	require.NoError(t, err)
	return st
}

func specStatuses(t *testing.T, d *specout.Generator) map[bridge.Key]map[int]bool {
	t.Helper()
	st, err := bridge.Statuses(d, true)
	require.NoError(t, err)
	return st
}

func keys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

func schemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	s, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)
	require.True(t, ok, "no components.schemas in the document")
	return s
}

func props(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	s, ok := schemas(t, doc)[name].(map[string]any)
	require.True(t, ok, "no component %s", name)
	return s["properties"].(map[string]any)
}

func isNullable(p map[string]any) bool {
	if p["type"] == "null" {
		return true
	}
	for _, keyword := range []string{"oneOf", "anyOf"} {
		arms, _ := p[keyword].([]any)
		for _, arm := range arms {
			if schema, _ := arm.(map[string]any); schema["type"] == "null" {
				return true
			}
		}
	}
	typ, _ := p["type"].([]any)
	return slices.Contains(typ, any("null"))
}

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
