package specout_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type schemaGeneric[T any] struct {
	Value T `json:"value"`
}

type schemaGenericValue struct {
	ID string `json:"id"`
}

type schemaComposite string

func (schemaComposite) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string"}, {Type: "integer"}}}
}

type schemaSharedChild struct {
	Name      *string          `json:"name,omitempty"`
	Anything  *any             `json:"anything,omitempty"`
	Composite *schemaComposite `json:"composite,omitempty"`
}

type schemaSharedA struct {
	Child *schemaSharedChild `json:"child,omitempty"`
}

type schemaSharedB struct {
	Child schemaSharedChild `json:"child"`
}

type RecursiveEmbedded struct {
	*RecursiveEmbedded

	Value *string `json:"value,omitempty"`
}

type schemaMapBody struct {
	Typed map[string]string `json:"typed"`
	Free  map[string]any    `json:"free"`
}

type schemaEmail struct {
	Address string `json:"address"`
}

type schemaSlack struct {
	Channel string `json:"channel"`
}

type schemaUnion struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}

func TestGenericComponentsUseOneLegalNameForDefinitionsAndRefs(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/generic", specout.Handler[struct{}, schemaGeneric[schemaGenericValue]]{HandlerFunc: noop})
	components := schemas(t, doc)
	legal := regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	for name := range components {
		assert.Regexp(t, legal, name)
		assert.NotContains(t, name, "[")
	}
	for _, ref := range componentRefs(doc) {
		name, ok := strings.CutPrefix(ref, "#/components/schemas/")
		if ok {
			assert.Contains(t, components, name, "unresolved component ref %s", ref)
		}
	}
	require.Contains(t, components, "schemaGenericschemaGenericValue")
}

func TestNullableFixupIsIdempotentForSharedAndCompositeSchemas(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/a", specout.Handler[struct{}, schemaSharedA]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/b", specout.Handler[struct{}, schemaSharedB]{HandlerFunc: noop})
	doc := buildDoc(t, d)

	child := props(t, doc, "schemaSharedChild")
	assert.True(t, isNullable(child["name"].(map[string]any)))
	assert.Equal(t, true, child["anything"], "*any already accepts null")
	composite := child["composite"].(map[string]any)
	assert.True(t, isNullable(composite), "custom anyOf must gain one null arm")
	assert.Len(t, composite["anyOf"], 2)
	assert.Len(t, componentsSchema(t, doc, "schemaComposite")["anyOf"], 2, "custom schema must be preserved")
	assert.True(t, isNullable(props(t, doc, "schemaSharedA")["child"].(map[string]any)))
	assertNoEmptySchemaTypes(t, doc)
}

func TestNullableEnumAndConstKeepTheirConstraint(t *testing.T) {
	type request struct {
		Enabled *bool   `json:"enabled,omitempty" jsonschema:"enum=true"`
		Mode    *string `json:"mode,omitempty"    jsonschema:"enum=on"`
	}
	p := props(t, docOf(t, http.MethodPost, "/nullable", specout.Handler[request, specout.NoContent]{HandlerFunc: noop}), "request")
	assert.ElementsMatch(t, []any{true, nil}, p["enabled"].(map[string]any)["enum"])
	assert.ElementsMatch(t, []any{"on", nil}, p["mode"].(map[string]any)["enum"])
}

func TestRecursiveAnonymousPointerEmbeddingTerminates(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/recursive", specout.Handler[struct{}, RecursiveEmbedded]{HandlerFunc: noop})
	assert.PanicsWithValue(t, "specout: recursive anonymous embedding in specout_test.RecursiveEmbedded", func() {
		buildDoc(t, d)
	})
}

func componentsSchema(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	schema, ok := schemas(t, doc)[name].(map[string]any)
	require.True(t, ok, "no component %s", name)
	return schema
}

func TestClosedSchemasPreserveTypedAndFreeMaps(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	specout.Document(d, http.MethodPost, "/maps", specout.Handler[schemaMapBody, specout.NoContent]{HandlerFunc: noop})
	p := props(t, buildDoc(t, d), "schemaMapBody")

	typed := p["typed"].(map[string]any)
	assert.Equal(t, "string", typed["additionalProperties"].(map[string]any)["type"])
	assert.NotEqual(t, false, p["free"].(map[string]any)["additionalProperties"])
}

func TestRequestBodyViewDoesNotReplaceTheFullResponseSchema(t *testing.T) {
	type request struct {
		ID   string `path:"id"`
		Name string `json:"name"`
	}
	doc := docOf(t, http.MethodPost, "/requests/{id}", specout.Handler[request, request]{HandlerFunc: noop})
	component := componentsSchema(t, doc, "request")
	p := component["properties"].(map[string]any)
	assert.Contains(t, p, "ID", "response schema must keep the path-tagged field")
	assert.Contains(t, p, "name")
	bodyRef := opOf(t, doc, "/requests/{id}", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	assert.NotEqual(t, "#/components/schemas/request", bodyRef, "split request body needs a distinct component")
}

func TestNestedFullSchemaAlsoSeparatesARequestBodyView(t *testing.T) {
	type request struct {
		ID   string `path:"id"`
		Name string `json:"name"`
	}
	type envelope struct {
		Value request `json:"value"`
	}
	d := newGen()
	specout.Document(d, http.MethodPost, "/requests/{id}", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/requests", specout.Handler[struct{}, envelope]{HandlerFunc: noop})
	doc := buildDoc(t, d)

	assert.Contains(t, componentsSchema(t, doc, "request")["properties"], "ID")
	bodyRef := opOf(t, doc, "/requests/{id}", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	assert.Equal(t, "#/components/schemas/requestBody", bodyRef)
}

func TestBooleanEnumTagIsPreserved(t *testing.T) {
	type response struct {
		Success bool `json:"success" jsonschema:"enum=true"`
	}
	type request struct {
		Enabled bool `jsonschema:"enum=false" query:"enabled"`
	}
	doc := docOf(t, http.MethodGet, "/bool", specout.Handler[request, response]{HandlerFunc: noop})
	assert.Equal(t, []any{true}, props(t, doc, "response")["success"].(map[string]any)["enum"])
	parameter := paramsOf(t, opOf(t, doc, "/bool", "get"))["enabled"]
	assert.Equal(t, []any{false}, parameter["schema"].(map[string]any)["enum"])
}

func TestUnionConstrainsTheCompleteEnvelope(t *testing.T) {
	d := newGen()
	d.Register[schemaEmail]("email")
	d.Register[schemaSlack]("slack")
	specout.Document(d, http.MethodPost, "/union", specout.Handler[schemaUnion, specout.NoContent]{HandlerFunc: noop})
	components := schemas(t, buildDoc(t, d))
	union := components["schemaUnion"].(map[string]any)

	assert.NotContains(t, union, "properties")
	arms := union["oneOf"].([]any)
	require.Len(t, arms, 2)
	mapping := union["discriminator"].(map[string]any)["mapping"].(map[string]any)
	for i, tc := range []struct {
		kind, data string
	}{{"email", "schemaEmail"}, {"slack", "schemaSlack"}} {
		ref := arms[i].(map[string]any)["$ref"].(string)
		assert.Equal(t, ref, mapping[tc.kind])
		branch := components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		assert.ElementsMatch(t, []any{"kind", "data"}, branch["required"])
		p := branch["properties"].(map[string]any)
		assert.Equal(t, tc.kind, p["kind"].(map[string]any)["const"])
		assert.Equal(t, "#/components/schemas/"+tc.data, p["data"].(map[string]any)["$ref"])
	}
}

func componentRefs(v any) []string {
	var refs []string
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, _ := x["$ref"].(string); ref != "" {
				refs = append(refs, ref)
			}
			for _, child := range x {
				visit(child)
			}
		case []any:
			for _, child := range x {
				visit(child)
			}
		}
	}
	visit(v)
	return refs
}

func assertNoEmptySchemaTypes(t *testing.T, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		if types, ok := x["type"].([]any); ok {
			assert.NotContains(t, types, "")
		}
		for _, child := range x {
			assertNoEmptySchemaTypes(t, child)
		}
	case []any:
		for _, child := range x {
			assertNoEmptySchemaTypes(t, child)
		}
	}
}
