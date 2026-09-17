package specout_test

import (
	"testing"

	"github.com/happytoolin/specout"
)

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

	doc := docOf(t, "POST", "/x", specout.Handler[Req, specout.NoContent]{HandlerFunc: noop})
	p := props(t, doc, "Req")
	if len(p) != 3 {
		t.Fatalf("body props = %v, want id, flags, name", p)
	}
	if p["flags"].(map[string]any)["readOnly"] != true {
		t.Errorf("embedded readOnly lost: %v", p["flags"])
	}
	if !isNullable(p["id"].(map[string]any)) {
		t.Errorf("embedded pointer not nullable: %v", p["id"])
	}
	if _, ok := p["page"]; ok {
		t.Error("query param leaked into the body")
	}
	if params := paramsOf(t, opOf(t, doc, "/x", "post")); len(params) != 1 || params["page"] == nil {
		t.Errorf("parameters = %v, want [page]", params)
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

	p := props(t, docOf(t, "POST", "/x", specout.Handler[struct{}, Res]{HandlerFunc: noop}), "Res")
	for _, name := range []string{"id", "flags", "score", "name"} {
		if _, ok := p[name]; !ok {
			t.Fatalf("promoted property %s missing: %v", name, p)
		}
	}
	if p["flags"].(map[string]any)["readOnly"] != true {
		t.Errorf("promoted readOnly lost: %v", p["flags"])
	}
	for _, name := range []string{"id", "score"} {
		if !isNullable(p[name].(map[string]any)) {
			t.Errorf("promoted pointer %s not nullable: %v", name, p[name])
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

	p := props(t, docOf(t, "POST", "/x", specout.Handler[struct{}, Res]{HandlerFunc: noop}), "Res")
	if items := p["slice"].(map[string]any)["items"].(map[string]any); !isNullable(items) {
		t.Errorf("slice element not nullable: %v", items)
	}
	if add := p["map"].(map[string]any)["additionalProperties"].(map[string]any); !isNullable(add) {
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

	p := props(t, docOf(t, "POST", "/x", specout.Handler[Req, specout.NoContent]{HandlerFunc: noop}), "Req")
	if len(p) != 1 {
		t.Fatalf("body props = %v, want only the shadowing id", p)
	}
	typ, _ := p["id"].(map[string]any)["type"].([]any)
	if len(typ) != 2 || typ[0] != "integer" {
		t.Errorf("id = %v, want the direct *int64 field to win", p["id"])
	}
}
