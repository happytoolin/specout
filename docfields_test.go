package specout_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

type docReq struct {
	Session string `cookie:"session"`
	Trace   string `header:"X-Trace-Id"`
	Body    string `json:"body"`
}

type docRes struct {
	OK bool `json:"ok"`
}

// TestDocFields: cookie/header params, Description, operationId, auth
// scheme, Public opt-out, ExternalDocs.
func TestDocFields(t *testing.T) {
	d := specout.New(specout.Config{
		Title:   "t",
		Version: "1",
		Auth: []specout.AuthScheme{
			specout.Bearer,
			specout.APIKey("apiKey", "X-API-Key", specout.InHeader),
		},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example"},
	})
	r := chi.NewRouter()
	specout.Chi(d, r).Post("/things", specout.Handler[docReq, docRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
		Summary:     "s",
		Description: "longer description",
	})
	specout.Chi(d, r).Get("/ping", specout.Handler[struct{}, docRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
		Public:      true,
	})
	if err := specout.Chi(d, r).Adopt(); err != nil {
		t.Fatal(err)
	}
	r.Mount("/openapi.json", d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc map[string]any
	json.Unmarshal(w.Body.Bytes(), &doc)

	// cookie + header params
	post := doc["paths"].(map[string]any)["/things"].(map[string]any)["post"].(map[string]any)
	params := post["parameters"].([]any)
	got := map[string]string{}
	for _, p := range params {
		m := p.(map[string]any)
		got[m["in"].(string)] = m["name"].(string)
	}
	if got["cookie"] != "session" || got["header"] != "X-Trace-Id" {
		t.Errorf("params = %v", got)
	}
	if post["description"] != "longer description" {
		t.Error("description missing")
	}
	if post["operationId"] != "postThings" {
		t.Errorf("operationId = %v", post["operationId"])
	}
	if _, ok := post["security"]; ok {
		t.Error("non-public route should inherit top-level security")
	}

	// schemes + top-level security alternatives (OR semantics)
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	bearer := schemes["bearerAuth"].(map[string]any)
	if bearer["type"] != "http" || bearer["scheme"] != "bearer" {
		t.Errorf("bearer = %v", bearer)
	}
	apikey := schemes["apiKey"].(map[string]any)
	if apikey["type"] != "apiKey" || apikey["name"] != "X-API-Key" || apikey["in"] != "header" {
		t.Errorf("apiKey = %v", apikey)
	}
	sec := doc["security"].([]any)
	if len(sec) != 2 {
		t.Errorf("security alternatives = %d, want 2", len(sec))
	}

	// public route: security: []
	ping := doc["paths"].(map[string]any)["/ping"].(map[string]any)["get"].(map[string]any)
	if sec, ok := ping["security"]; !ok || len(sec.([]any)) != 0 {
		t.Errorf("public route security = %v", ping["security"])
	}

	// externalDocs
	if doc["externalDocs"].(map[string]any)["url"] != "https://x.example" {
		t.Error("externalDocs missing")
	}
}
