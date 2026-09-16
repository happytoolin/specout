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
	if err := specout.Chi(d, r).Adopt(); err != nil {
		t.Fatalf("adopt: %v", err)
	}
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

// recA and recB recurse through each other; package-level because local type
// declarations are scoped from the point of declaration, so neither can name
// the other.
type recA struct {
	B  *recB `json:"b,omitempty"`
	ID *int  `json:"id,omitempty"`
}

type recB struct {
	A *recA `json:"a,omitempty"`
}

type recPair struct {
	Left recA `json:"left"`
}

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
	specout.Chi(d, r).Get("/issues", specout.Handler[struct{}, Page]{HandlerFunc: noop2})
	specout.Chi(d, r).Post("/issues", specout.Handler[struct {
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

// Recursive types reach themselves through a $ref. The fixup walk must not
// loop, and each component must still get its pass, including when two types
// recurse through each other.
func TestRecursiveNestedDefsFixed(t *testing.T) {
	type Node struct {
		Name string `json:"name"`
		Next *Node  `json:"next,omitempty"`
	}
	type Tree struct {
		Root Node `json:"root"`
	}
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/tree", specout.Handler[struct{}, Tree]{HandlerFunc: noop2})
	specout.Chi(d, r).Get("/pair", specout.Handler[struct{}, recPair]{HandlerFunc: noop2})
	doc := serveDoc(t, d, r)

	wantNullable := func(name, prop string) {
		t.Helper()
		schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
		p := schemas[name].(map[string]any)["properties"].(map[string]any)[prop].(map[string]any)
		if arms, _ := p["oneOf"].([]any); len(arms) == 2 {
			return
		}
		if typ, _ := p["type"].([]any); len(typ) == 2 && typ[1] == "null" {
			return
		}
		t.Errorf("%s.%s = %v, want a null arm", name, prop, p)
	}
	wantNullable("Node", "next")
	wantNullable("recA", "b")
	wantNullable("recA", "id")
	wantNullable("recB", "a")
}

// Regression: a nested-only type — never a top-level Req or Res — is hoisted
// to $defs before the fixup pass runs. The pass must follow the $ref into
// that component: without the walk the whole nested type keeps invopop's raw
// output, so its pointer fields are not nullable and its readonly/deprecated
// tags vanish. TestNestedDefsKeepFixups cannot catch this: it registers Issue
// top-level as well, and byType wins the name clash.
func TestNestedOnlyDefsKeepFixups(t *testing.T) {
	type Inner struct {
		ID   int64   `json:"id" jsonschema:"readonly"`
		Note string  `json:"note" jsonschema:"deprecated"`
		Due  *string `json:"due,omitempty"`
	}
	type Outer struct {
		Items []Inner `json:"items"`
	}

	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/outer", specout.Handler[struct{}, Outer]{HandlerFunc: noop2})

	doc := serveDoc(t, d, r)
	inner := doc["components"].(map[string]any)["schemas"].(map[string]any)["Inner"].(map[string]any)["properties"].(map[string]any)
	if inner["id"].(map[string]any)["readOnly"] != true {
		t.Errorf("nested readOnly lost: %v", inner["id"])
	}
	if inner["note"].(map[string]any)["deprecated"] != true {
		t.Errorf("nested deprecated lost: %v", inner["note"])
	}
	if typ, _ := inner["due"].(map[string]any)["type"].([]any); len(typ) != 2 || typ[1] != "null" {
		t.Errorf("nested nullable lost: %v", inner["due"])
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
	specout.Chi(d, r).Get("/items", specout.Handler[ListReq, specout.NoContent]{HandlerFunc: noop2})
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
	specout.Chi(d, r).Post("/a", specout.Handler[struct {
		A string `json:"a"`
	}, specout.NoContent]{HandlerFunc: noop2})
	specout.Chi(d, r).Post("/b", specout.Handler[struct {
		B string `json:"b"`
	}, specout.NoContent]{HandlerFunc: noop2})
	doc := serveDoc(t, d, r)

	for name := range doc["components"].(map[string]any)["schemas"].(map[string]any) {
		if len(name) >= 6 && name[:6] == "struct" {
			t.Errorf("raw struct literal leaked as component name: %s", name)
		}
	}
}
