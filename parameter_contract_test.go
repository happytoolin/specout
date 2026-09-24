package specout_test

import (
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParameterIgnoresJSONExclusion(t *testing.T) {
	type request struct {
		Limit *int  `json:"-" jsonschema:"minimum=1,description=Page size"         query:"limit,omitempty"`
		ID    int64 `json:"-" jsonschema:"format=int64,style=simple,explode=false" path:"id"`
	}
	d := docOf(t, "GET", "/items/{id}", specout.Handler[request, string]{HandlerFunc: noop})
	op := opOf(t, d, "/items/{id}", "get")
	params := paramsOf(t, op)
	assert.NotContains(t, op, "requestBody")
	assert.Equal(t, "Page size", params["limit"]["description"])
	limit := params["limit"]["schema"].(map[string]any)
	assert.True(t, isNullable(limit))
	assert.InDelta(t, 1, limit["minimum"], 0)
	assert.Equal(t, "integer", params["id"]["schema"].(map[string]any)["type"])
	assert.Equal(t, "simple", params["id"]["style"])
	assert.Equal(t, false, params["id"]["explode"])
}

func TestParameterEnumsSplitBeforeNullability(t *testing.T) {
	type request struct {
		ID     string  `jsonschema:"enum=a|b"     path:"id"`
		Query  *string `jsonschema:"enum=a|b"     query:"v,omitempty"`
		Header *string `header:"X-Mode,omitempty" jsonschema:"enum=a|b"`
		Cookie *string `cookie:"mode,omitempty"   jsonschema:"enum=a|b"`
	}
	doc := docOf(t, "GET", "/items/{id}", specout.Handler[request, string]{HandlerFunc: noop})
	parameters := paramsOf(t, opOf(t, doc, "/items/{id}", "get"))
	assert.Equal(t, []any{"a", "b"}, parameters["id"]["schema"].(map[string]any)["enum"])
	for _, name := range []string{"v", "X-Mode", "mode"} {
		schema := parameters[name]["schema"].(map[string]any)
		assert.Equal(t, []any{"a", "b", nil}, schema["enum"])
		assert.True(t, isNullable(schema))
	}
}

func TestParameterLocationsKeepTheirContracts(t *testing.T) {
	type request struct {
		ID     *int `jsonschema:"description=Item ID,style=simple,explode=false" path:"id,omitempty"`
		Limit  int  `jsonschema:"description=Page size,style=form,default=7"     query:"limit"`
		Header *int `header:"X-Count"                                            jsonschema:"description=Header count,style=simple"`
		Cookie *int `cookie:"count,omitempty"                                    jsonschema:"description=Cookie count,style=form"`
	}
	doc := docOf(t, "GET", "/items/{id}/{untyped}", specout.Handler[request, string]{HandlerFunc: noop})
	parameters := paramsOf(t, opOf(t, doc, "/items/{id}/{untyped}", "get"))
	for _, tc := range []struct {
		name, location, description, style string
		required, nullable                 bool
	}{
		{"id", "path", "Item ID", "simple", true, false},
		{"untyped", "path", "", "", true, false},
		{"limit", "query", "Page size", "form", false, false},
		{"X-Count", "header", "Header count", "simple", true, true},
		{"count", "cookie", "Cookie count", "form", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parameters[tc.name]
			assert.Equal(t, tc.location, p["in"])
			assert.Equal(t, tc.required, p["required"])
			schema := p["schema"].(map[string]any)
			assert.Equal(t, tc.nullable, isNullable(schema))
			assert.NotContains(t, schema, "description")
			if tc.description != "" {
				assert.Equal(t, tc.description, p["description"])
				assert.Equal(t, tc.style, p["style"])
			}
		})
	}
	assert.Equal(t, false, parameters["id"]["explode"])
	assert.Equal(t, "string", parameters["untyped"]["schema"].(map[string]any)["type"])
	assert.InDelta(t, 7, parameters["limit"]["schema"].(map[string]any)["default"], 0)
}

func TestEmbeddedParametersUseSameBodyView(t *testing.T) {
	type page struct {
		Limit int    `query:"limit,omitempty"`
		Trace string `header:"X-Trace,omitempty"`
	}
	type body struct {
		Name string `json:"name"`
	}
	type request struct {
		*page
		body

		ID int `path:"id"`
	}
	d := docOf(t, "POST", "/items/{id}", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	op := opOf(t, d, "/items/{id}", "post")
	params := paramsOf(t, op)
	require.Len(t, params, 3)
	assert.Equal(t, "query", params["limit"]["in"])
	assert.Equal(t, "header", params["X-Trace"]["in"])
	assert.Equal(t, map[string]any{"name": map[string]any{"type": "string"}}, props(t, d, "requestBody"))
}

func TestEmbeddedParameterAmbiguityAndShadowing(t *testing.T) {
	type left struct {
		Limit int `query:"left"`
	}
	type right struct {
		Limit int `query:"right"`
	}
	type ambiguous struct {
		left
		right

		Name string `json:"name"`
		Q    string `query:"q"`
	}
	d := docOf(t, "POST", "/x", specout.Handler[ambiguous, specout.NoContent]{HandlerFunc: noop})
	assert.Len(t, paramsOf(t, opOf(t, d, "/x", "post")), 1)
	assert.NotContains(t, props(t, d, "ambiguousBody"), "Limit")
	type shadowed struct {
		left

		Limit string `query:"direct"`
	}
	d = docOf(t, "GET", "/x", specout.Handler[shadowed, string]{HandlerFunc: noop})
	p := paramsOf(t, opOf(t, d, "/x", "get"))
	require.Len(t, p, 1)
	assert.Contains(t, p, "direct")
}

func TestNamedEmbeddedObjectKeepsNestedParamsInBody(t *testing.T) {
	type nested struct {
		Limit int `json:"limit" query:"limit"`
	}
	type request struct {
		Nested nested `json:"nested"`
		Q      string `query:"q"`
	}
	d := docOf(t, "POST", "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	assert.Len(t, paramsOf(t, opOf(t, d, "/x", "post")), 1)
	assert.Contains(t, props(t, d, "requestBody"), "nested")
}

func TestEmbeddedSameGoNameKeepsDistinctJSONFields(t *testing.T) {
	type left struct {
		ID string `json:"left,omitempty"`
	}
	type right struct {
		ID string `json:"right,omitzero"`
	}
	type request struct {
		left
		right

		Field2 string `json:",omitempty"`
		Q      string `query:"q"`
	}
	doc := docOf(t, "POST", "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	p := props(t, doc, "requestBody")
	assert.Contains(t, p, "left")
	assert.Contains(t, p, "right")
	assert.Contains(t, p, "Field2")
	assert.NotContains(t, schemas(t, doc)["requestBody"], "required")
}

func TestInvalidParameterDeclarationsFail(t *testing.T) {
	type duplicate struct {
		A string `query:"q"`
		B string `query:"q"`
	}
	type badStyle struct {
		A string `header:"X-Value" jsonschema:"style=deepObject"`
	}
	assert.PanicsWithValue(t, "specout: empty or duplicate query parameter q", func() { docOf(t, "GET", "/x", specout.Handler[duplicate, string]{HandlerFunc: noop}) })
	assert.PanicsWithValue(t, "specout: field A has invalid header parameter style=deepObject", func() { docOf(t, "GET", "/x", specout.Handler[badStyle, string]{HandlerFunc: noop}) })
}
