package specout_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
)

// oneDoc renders a single Document-registered route into its whole document.
func oneDoc[Req, Res any](t *testing.T, h specout.Handler[Req, Res]) map[string]any {
	t.Helper()
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Document(d, "POST", "/x", h)
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func docProps(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	s, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)[name].(map[string]any)
	if !ok {
		t.Fatalf("no component %s", name)
	}
	return s["properties"].(map[string]any)
}

func isNullable(p map[string]any) bool {
	if arms, _ := p["oneOf"].([]any); len(arms) == 2 {
		return true
	}
	typ, _ := p["type"].([]any)
	return len(typ) == 2 && typ[1] == "null"
}

// Regression: a body struct that embeds a base type and also carries a query
// parameter used to panic in reflect.StructOf (the body view copies fields
// verbatim, and StructOf rejects an unexported embedded field). The embedded
// fields must still flatten into the body and keep their fixups.
func TestEmbeddedBodyWithParam(t *testing.T) {
	type base struct {
		ID    *int64  `json:"id,omitempty"`
		Flags *string `json:"flags,omitempty" jsonschema:"readonly"`
	}
	type Req struct {
		base
		Name *string `json:"name,omitempty"`
		Page int     `query:"page"`
	}

	doc := oneDoc[Req, specout.NoContent](t, specout.Handler[Req, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
	})
	props := docProps(t, doc, "Req")
	if len(props) != 3 {
		t.Fatalf("body props = %v, want id, flags, name", props)
	}
	if props["flags"].(map[string]any)["readOnly"] != true {
		t.Errorf("embedded readOnly lost: %v", props["flags"])
	}
	if !isNullable(props["id"].(map[string]any)) {
		t.Errorf("embedded pointer not nullable: %v", props["id"])
	}
	if _, ok := props["page"]; ok {
		t.Error("query param leaked into the body")
	}
	op := doc["paths"].(map[string]any)["/x"].(map[string]any)["post"].(map[string]any)
	var names []string
	for _, p := range op["parameters"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	if len(names) != 1 || names[0] != "page" {
		t.Errorf("parameters = %v, want [page]", names)
	}
}

// Regression: an untagged embedded struct is flattened by invopop, so its
// fields are properties of the parent schema, not of a nested object. The
// fixup walk must recurse into the parent schema for them, and must not skip
// an embedded field just because its Go name is unexported.
func TestEmbeddedFieldsKeepFixups(t *testing.T) {
	type embed struct {
		Score *int64 `json:"score,omitempty"`
	}
	type embedded struct {
		ID    *int64  `json:"id,omitempty"`
		Flags *string `json:"flags,omitempty" jsonschema:"readonly"`
	}
	type Res struct {
		embedded
		embed
		Name string `json:"name"`
	}

	doc := oneDoc[struct{}, Res](t, specout.Handler[struct{}, Res]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
	})
	props := docProps(t, doc, "Res")
	for _, p := range []string{"id", "flags", "score", "name"} {
		if _, ok := props[p]; !ok {
			t.Fatalf("promoted property %s missing: %v", p, props)
		}
	}
	if props["flags"].(map[string]any)["readOnly"] != true {
		t.Errorf("promoted readOnly lost: %v", props["flags"])
	}
	for _, p := range []string{"id", "score"} {
		if !isNullable(props[p].(map[string]any)) {
			t.Errorf("promoted pointer %s not nullable: %v", p, props[p])
		}
	}
}

// Regression: a pointer element of a slice or map is nullable like any other
// pointer, not only a pointer to the whole collection.
func TestPointerElementsNullable(t *testing.T) {
	type Item struct {
		P *string `json:"p,omitempty"`
	}
	type Res struct {
		Slice []*Item          `json:"slice"`
		Map   map[string]*Item `json:"map"`
	}

	doc := oneDoc[struct{}, Res](t, specout.Handler[struct{}, Res]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
	})
	props := docProps(t, doc, "Res")
	items := props["slice"].(map[string]any)["items"].(map[string]any)
	if !isNullable(items) {
		t.Errorf("slice element not nullable: %v", items)
	}
	add := props["map"].(map[string]any)["additionalProperties"].(map[string]any)
	if !isNullable(add) {
		t.Errorf("map value not nullable: %v", add)
	}
}

// Regression: a body that embeds a base type and shadows one of its fields
// panicked in reflect.StructOf (duplicate field name). Go's rule — the direct
// field shadows the promoted one — must hold, and duplicate names are not
// allowed through to StructOf.
func TestEmbeddedShadowingField(t *testing.T) {
	type base struct {
		ID *string `json:"id"`
	}
	type Req struct {
		base
		ID *int64 `json:"id"`
		Q  string `query:"q"`
	}

	doc := oneDoc[Req, specout.NoContent](t, specout.Handler[Req, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
	})
	props := docProps(t, doc, "Req")
	if len(props) != 1 {
		t.Fatalf("body props = %v, want only the shadowing id", props)
	}
	typ, _ := props["id"].(map[string]any)["type"].([]any)
	if len(typ) != 2 || typ[0] != "integer" {
		t.Errorf("id = %v, want the direct *int64 field to win", props["id"])
	}
}
