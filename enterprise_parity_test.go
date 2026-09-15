package specout_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

func serveDoc(t *testing.T, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	r.Mount("/openapi.json", d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != 200 {
		t.Fatalf("openapi.json status %d", w.Code)
	}
	var doc map[string]any
	json.Unmarshal(w.Body.Bytes(), &doc)
	return doc
}

func noop2(http.ResponseWriter, *http.Request) {}

// Regression: nested named types hoisted to $defs must keep jsonschema
// fixups (enum split, readonly, nullable) and must not overwrite the
// properly-reflected component registered later under the same name.
func TestNestedDefsKeepFixups(t *testing.T) {
	type Label struct {
		Color string `json:"color" jsonschema:"pattern=^[0-9a-f]{6}$"`
	}
	type Issue struct {
		ID    int64      `json:"id" jsonschema:"readonly"`
		State string     `json:"state" jsonschema:"enum=open|closed,default=open"`
		Label Label      `json:"label"`
		Due   *time.Time `json:"due,omitempty"`
	}
	type Page struct {
		Items []Issue `json:"items"`
	}

	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	d.Get(r, "/issues", specout.Handler[struct{}, Page]{HandlerFunc: noop2})
	d.Post(r, "/issues", specout.Handler[struct {
		Title string `json:"title"`
	}, Issue]{HandlerFunc: noop2})

	doc := serveDoc(t, d, r)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)

	issue := schemas["Issue"].(map[string]any)
	props := issue["properties"].(map[string]any)
	state := props["state"].(map[string]any)
	enum, _ := state["enum"].([]any)
	if len(enum) != 2 || enum[0] != "open" || enum[1] != "closed" {
		t.Fatalf("state enum = %v, want [open closed]", state["enum"])
	}
	if props["id"].(map[string]any)["readOnly"] != true {
		t.Error("id readOnly lost")
	}
	due := props["due"].(map[string]any)
	typ, _ := due["type"].([]any)
	if len(typ) != 2 || typ[1] != "null" {
		t.Errorf("due type = %v, want [string null]", due["type"])
	}
}

// Regression: query params must carry jsonschema keywords from the field
// tag (enum, min/max, default), not just the bare type.
func TestParamKeywords(t *testing.T) {
	type ListReq struct {
		Limit int    `query:"limit" jsonschema:"default=20,minimum=1,maximum=100"`
		Sort  string `query:"sort" jsonschema:"enum=created|updated,default=created"`
	}
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	d.Get(r, "/items", specout.Handler[ListReq, specout.NoContent]{HandlerFunc: noop2})
	doc := serveDoc(t, d, r)
	op := doc["paths"].(map[string]any)["/items"].(map[string]any)["get"].(map[string]any)

	schemas := map[string]map[string]any{}
	for _, raw := range op["parameters"].([]any) {
		p := raw.(map[string]any)
		schemas[p["name"].(string)] = p["schema"].(map[string]any)
	}
	limit := schemas["limit"]
	if limit["minimum"].(float64) != 1 || limit["maximum"].(float64) != 100 || limit["default"].(float64) != 20 {
		t.Errorf("limit schema = %v", limit)
	}
	sortS := schemas["sort"]
	enum, _ := sortS["enum"].([]any)
	if len(enum) != 2 || enum[0] != "created" || enum[1] != "updated" {
		t.Errorf("sort schema = %v", sortS)
	}
}

// Regression: anonymous Req/Res struct types must get clean deterministic
// component names, not the raw Go struct literal.
func TestAnonymousComponentNames(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	d.Post(r, "/a", specout.Handler[struct {
		A string `json:"a"`
	}, specout.NoContent]{HandlerFunc: noop2})
	d.Post(r, "/b", specout.Handler[struct {
		B string `json:"b"`
	}, specout.NoContent]{HandlerFunc: noop2})
	doc := serveDoc(t, d, r)

	for name := range doc["components"].(map[string]any)["schemas"].(map[string]any) {
		if len(name) >= 6 && name[:6] == "struct" {
			t.Errorf("raw struct literal leaked as component name: %s", name)
		}
	}
}
