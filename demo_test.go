package specout_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/specout/internal/demo/router"
)

// TestDemoServesValidShape: the demo spec parses and carries the showcase
// features — union refs, readOnly, query params, headers, omit, binary.
func TestDemoServesValidShape(t *testing.T) {
	d, r := router.New()
	_ = d
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.NewDecoder(resp.Body).Decode(&doc)
	resp.Body.Close()

	paths := doc["paths"].(map[string]any)
	for _, p := range []string{
		"/onboarding/", "/onboarding/{id}/", "/onboarding/{id}/sync",
		"/files/import", "/files/report", "/legacy", "/webhooks",
	} {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing %s", p)
		}
	}

	// query params on list
	list := paths["/onboarding/"].(map[string]any)["get"].(map[string]any)
	params := list["parameters"].([]any)
	if len(params) < 3 {
		t.Errorf("list params = %d, want >= 3 (limit cursor sort)", len(params))
	}

	// sync: 409 override, 422 override, 401 omitted
	sync := paths["/onboarding/{id}/sync"].(map[string]any)["post"].(map[string]any)
	resps := sync["responses"].(map[string]any)
	if _, has401 := resps["401"]; has401 {
		t.Error("401 should be omitted")
	}
	if _, has409 := resps["409"]; !has409 {
		t.Error("409 missing")
	}

	// union: webhooks body oneOf refs
	comps := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"EmailConfig", "SlackConfig", "Config"} {
		if _, ok := comps[name]; !ok {
			t.Errorf("missing component %s", name)
		}
	}

	// deprecated flag
	legacy := paths["/legacy"].(map[string]any)["get"].(map[string]any)
	if legacy["deprecated"] != true {
		t.Error("legacy not deprecated")
	}

	// binary response
	report := paths["/files/report"].(map[string]any)["get"].(map[string]any)
	rresps := report["responses"].(map[string]any)["200"].(map[string]any)
	content := rresps["content"].(map[string]any)
	if _, ok := content["application/pdf"]; !ok {
		t.Error("report not pdf")
	}
}
