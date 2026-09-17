package specout_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type Onboarding struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Stage string `json:"stage"`
}

type UpsertRequest struct {
	Owner string `json:"owner"`
	Stage string `json:"stage"`
}

type EmptyReq struct{}

// TestQuickStartFlow mirrors docs/api-reference.html §01: registrations on
// the root router with full patterns.
func TestQuickStartFlow(t *testing.T) {
	d, r := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "Internal onboarding service.",
		ErrorType:     Problem{},
		DefaultErrors: []int{400, 404, 500},
	}), chi.NewRouter()

	rc := specout.Chi(d, r)
	rc.Delete("/onboarding/{id}", specout.Handler[EmptyReq, struct{}]{HandlerFunc: okBody})
	rc.Put("/onboarding/{id}", specout.Handler[UpsertRequest, Onboarding]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusCreated}},
	})
	rc.Get("/onboarding", specout.Handler[EmptyReq, []Onboarding]{HandlerFunc: noop, Summary: "List onboarding"})
	adopt(t, rc)
	r.Mount("/openapi.json", d)

	fetch := func() []byte {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		require.Equal(t, 200, w.Code, "openapi.json status")
		return w.Body.Bytes()
	}
	body := fetch()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(body, &doc))

	require.Equal(t, "3.1.0", doc["openapi"])
	paths := pathsObj(doc)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		assert.Contains(t, paths, p, "missing path %q", p)
	}

	// 204 for NoContent, 200+201 refs for the upsert, global errors stamped
	assert.Contains(t, opOf(t, doc, "/onboarding/{id}", "delete")["responses"].(map[string]any), "204", "delete missing 204")
	putResps := opOf(t, doc, "/onboarding/{id}", "put")["responses"].(map[string]any)
	assert.Contains(t, putResps, "201", "put missing 201")
	for _, code := range []string{"400", "404", "500"} {
		assert.Contains(t, putResps, code, "put missing global default")
	}

	// component dedupe: Onboarding emitted once, referenced at 200 and 201
	assert.Contains(t, schemas(t, doc), "Onboarding", "missing Onboarding component")
	ref := func(code string) any {
		return putResps[code].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	}
	assert.Equal(t, "#/components/schemas/Onboarding", ref("200"), "dedupe 200")
	assert.Equal(t, ref("200"), ref("201"), "dedupe 201")

	// determinism: second serve is byte-identical
	assert.True(t, bytes.Equal(body, fetch()), "spec not deterministic across serves")

	// post-freeze registration panics
	require.Panics(t, func() { rc.Get("/late", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: noop}) })
}

// TestGroupsAndMountsResolve: Route groups and Mount'd subrouters resolve to
// full paths when the generator adopts the serving root (d.Adopt).
func TestGroupsAndMountsResolve(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	r.Route("/onboarding", func(r chi.Router) { specout.Chi(d, r).Get("/", okGet) })
	adopt(t, specout.Chi(d, r))
	assert.Contains(t, docPaths(t, d), "/onboarding/", "group route not resolved to full path")
}

// TestAdoptRejectsStrays: Adopt reports plain-handler routes the generator
// never registered.
func TestAdoptRejectsStrays(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/known", okGet)
	r.Get("/stray", func(http.ResponseWriter, *http.Request) {})
	require.ErrorContains(t, specout.Chi(d, r).Adopt(), "/stray")
}

func TestServeOnlyGet(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/x", okGet)
	adopt(t, specout.Chi(d, r))

	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/openapi.json", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "POST")

	rec = httptest.NewRecorder()
	d.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "GET")
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestStdMuxHandle(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	specout.Std(d, mux).Handle("DELETE /onboarding/{id}", specout.Handler[EmptyReq, struct{}]{HandlerFunc: okBody})
	specout.Std(d, mux).Handle("GET /onboarding", specout.Handler[EmptyReq, []Onboarding]{HandlerFunc: noop})

	doc := buildDoc(t, d)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		assert.Contains(t, pathsObj(doc), p, "missing std path %q", p)
	}
	assert.Contains(t, opOf(t, doc, "/onboarding/{id}", "delete")["responses"].(map[string]any), "204", "std delete missing 204")
}
