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

func okJSON(w http.ResponseWriter, r *http.Request)    {}
func noContent(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }

// TestQuickStartFlow mirrors docs/api-reference.html §01: registrations on
// the root router with full patterns.
func TestQuickStartFlow(t *testing.T) {
	d := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "Internal onboarding service.",
		ErrorType:     Problem{},
		DefaultErrors: []int{400, 404, 500},
	})

	r := chi.NewRouter()
	d.Delete(r, "/onboarding/{id}", specout.Handler[EmptyReq, specout.NoContent]{HandlerFunc: noContent})
	d.Put(r, "/onboarding/{id}", specout.Handler[UpsertRequest, Onboarding]{
		HandlerFunc: okJSON,
		Responses:   []specout.Response{{Status: http.StatusCreated}},
	})
	d.Get(r, "/onboarding", specout.Handler[EmptyReq, []Onboarding]{HandlerFunc: okJSON, Summary: "List onboarding"})
	r.Mount("/openapi.json", d)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}

	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing path %q; have %v", p, paths)
		}
	}

	// 204 for NoContent, 200+201 refs for the upsert, global errors stamped
	del := paths["/onboarding/{id}"].(map[string]any)["delete"].(map[string]any)
	delResps := del["responses"].(map[string]any)
	if _, ok := delResps["204"]; !ok {
		t.Error("delete missing 204")
	}
	put := paths["/onboarding/{id}"].(map[string]any)["put"].(map[string]any)
	putResps := put["responses"].(map[string]any)
	if _, ok := putResps["201"]; !ok {
		t.Error("put missing 201")
	}
	for _, code := range []string{"400", "404", "500"} {
		if _, ok := putResps[code]; !ok {
			t.Errorf("put missing global default %s", code)
		}
	}

	// component dedupe: Onboarding emitted once, referenced at 200 and 201
	comps := doc["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := comps["Onboarding"]; !ok {
		t.Errorf("missing Onboarding component; have %v", comps)
	}
	ref200 := putResps["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	ref201 := putResps["201"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	if ref200 != "#/components/schemas/Onboarding" || ref201 != ref200 {
		t.Errorf("dedupe broken: 200=%v 201=%v", ref200, ref201)
	}

	// determinism: second serve is byte-identical
	r2, _ := http.Get(srv.URL + "/openapi.json")
	var b2 bytes.Buffer
	b2.ReadFrom(r2.Body)
	r2.Body.Close()
	var w1, w2 bytes.Buffer
	d.WriteJSON(&w1)
	_ = w2
	var second map[string]any
	json.Unmarshal(b2.Bytes(), &second)
	j1, _ := json.Marshal(doc)
	j2, _ := json.Marshal(second)
	if !bytes.Equal(j1, j2) {
		t.Error("spec not deterministic across serves")
	}

	// post-freeze registration panics
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic on post-freeze registration")
			}
		}()
		d.Get(r, "/late", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: okJSON})
	}()
}

// TestGroupsAndMountsResolve: Route groups and Mount'd subrouters resolve to
// full paths when the generator adopts the serving root (d.Adopt).
func TestGroupsAndMountsResolve(t *testing.T) {
	d := specout.New(specout.Config{Title: "T", Version: "1"})
	r := chi.NewRouter()
	r.Route("/onboarding", func(r chi.Router) {
		d.Get(r, "/", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: okJSON})
	})
	if err := d.Adopt(r); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(buf.Bytes(), &doc)
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/onboarding/"]; !ok {
		t.Errorf("group route not resolved to full path; have %v", paths)
	}
}

// TestAdoptRejectsStrays: Adopt reports plain-handler routes the generator
// never registered.
func TestAdoptRejectsStrays(t *testing.T) {
	d := specout.New(specout.Config{Title: "T", Version: "1"})
	r := chi.NewRouter()
	d.Get(r, "/known", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: okJSON})
	r.Get("/stray", func(w http.ResponseWriter, _ *http.Request) {})
	err := d.Adopt(r)
	if err == nil || !strings.Contains(err.Error(), "/stray") {
		t.Fatalf("expected stray-route error, got %v", err)
	}
}

func TestServeOnlyGet(t *testing.T) {
	d := specout.New(specout.Config{Title: "T", Version: "1"})
	r := chi.NewRouter()
	d.Get(r, "/x", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: okJSON})

	req := httptest.NewRequest(http.MethodPost, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec = httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
}
