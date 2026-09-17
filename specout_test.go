package specout_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
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
		if w.Code != 200 {
			t.Fatalf("status = %d", w.Code)
		}
		return w.Body.Bytes()
	}
	body := fetch()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}

	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
	paths := pathsObj(doc)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing path %q; have %v", p, paths)
		}
	}

	// 204 for NoContent, 200+201 refs for the upsert, global errors stamped
	if _, ok := opOf(t, doc, "/onboarding/{id}", "delete")["responses"].(map[string]any)["204"]; !ok {
		t.Error("delete missing 204")
	}
	putResps := opOf(t, doc, "/onboarding/{id}", "put")["responses"].(map[string]any)
	if _, ok := putResps["201"]; !ok {
		t.Error("put missing 201")
	}
	for _, code := range []string{"400", "404", "500"} {
		if _, ok := putResps[code]; !ok {
			t.Errorf("put missing global default %s", code)
		}
	}

	// component dedupe: Onboarding emitted once, referenced at 200 and 201
	if _, ok := schemas(t, doc)["Onboarding"]; !ok {
		t.Error("missing Onboarding component")
	}
	ref := func(code string) any {
		return putResps[code].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	}
	if ref200, ref201 := ref("200"), ref("201"); ref200 != "#/components/schemas/Onboarding" || ref201 != ref200 {
		t.Errorf("dedupe broken: 200=%v 201=%v", ref200, ref201)
	}

	// determinism: second serve is byte-identical
	if second := fetch(); !bytes.Equal(body, second) {
		t.Error("spec not deterministic across serves")
	}

	// post-freeze registration panics
	panics(t, func() { rc.Get("/late", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: noop}) })
}

// TestGroupsAndMountsResolve: Route groups and Mount'd subrouters resolve to
// full paths when the generator adopts the serving root (d.Adopt).
func TestGroupsAndMountsResolve(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	r.Route("/onboarding", func(r chi.Router) { specout.Chi(d, r).Get("/", okGet) })
	adopt(t, specout.Chi(d, r))
	if _, ok := docPaths(t, d)["/onboarding/"]; !ok {
		t.Errorf("group route not resolved to full path; have %v", docPaths(t, d))
	}
}

// TestAdoptRejectsStrays: Adopt reports plain-handler routes the generator
// never registered.
func TestAdoptRejectsStrays(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/known", okGet)
	r.Get("/stray", func(http.ResponseWriter, *http.Request) {})
	wantErr(t, specout.Chi(d, r).Adopt(), "/stray")
}

func TestServeOnlyGet(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/x", okGet)
	adopt(t, specout.Chi(d, r))

	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/openapi.json", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	d.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestStdMuxHandle(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	specout.Std(d, mux).Handle("DELETE /onboarding/{id}", specout.Handler[EmptyReq, struct{}]{HandlerFunc: okBody})
	specout.Std(d, mux).Handle("GET /onboarding", specout.Handler[EmptyReq, []Onboarding]{HandlerFunc: noop})

	doc := buildDoc(t, d)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		if _, ok := pathsObj(doc)[p]; !ok {
			t.Errorf("missing std path %q; have %v", p, pathsObj(doc))
		}
	}
	if _, ok := opOf(t, doc, "/onboarding/{id}", "delete")["responses"].(map[string]any)["204"]; !ok {
		t.Error("std delete missing 204")
	}
}
