package specout_test

import (
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.Len(t, p, 3, "body props = %v, want id, flags, name", p)
	assert.Equal(t, true, p["flags"].(map[string]any)["readOnly"], "embedded readOnly lost: %v", p["flags"])
	assert.True(t, isNullable(p["id"].(map[string]any)), "embedded pointer not nullable: %v", p["id"])
	assert.NotContains(t, p, "page", "query param leaked into the body")

	params := paramsOf(t, opOf(t, doc, "/x", "post"))
	assert.Len(t, params, 1, "parameters = %v, want [page]", params)
	assert.Contains(t, params, "page")
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
		require.Contains(t, p, name, "promoted property missing: %v", p)
	}
	assert.Equal(t, true, p["flags"].(map[string]any)["readOnly"], "promoted readOnly lost: %v", p["flags"])
	for _, name := range []string{"id", "score"} {
		assert.True(t, isNullable(p[name].(map[string]any)), "promoted pointer %s not nullable: %v", name, p[name])
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
	items := p["slice"].(map[string]any)["items"].(map[string]any)
	assert.True(t, isNullable(items), "slice element not nullable: %v", items)
	add := p["map"].(map[string]any)["additionalProperties"].(map[string]any)
	assert.True(t, isNullable(add), "map value not nullable: %v", add)
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
	require.Len(t, p, 1, "body props = %v, want only the shadowing id", p)
	typ, _ := p["id"].(map[string]any)["type"].([]any)
	assert.Len(t, typ, 2)
	assert.Equal(t, "integer", typ[0], "want the direct *int64 field to win")
}
