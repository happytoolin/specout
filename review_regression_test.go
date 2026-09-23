package specout_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reviewRequired struct {
	Name string `json:"name,omitempty"`
}

func (reviewRequired) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Required = []string{"name"}
}

func TestSchemaFixesPreserveCustomRequiredFields(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, reviewRequired]{HandlerFunc: noop})
	assert.Contains(t, componentsSchema(t, doc, "reviewRequired")["required"], "name")
}

func TestRegexAsteriskRouteIsDocumented(t *testing.T) {
	for _, adapter := range []string{"chi", "gorilla"} {
		t.Run(adapter, func(t *testing.T) {
			d := newGen()
			var router http.Handler
			if adapter == "chi" {
				r := chi.NewRouter()
				b := specout.Chi(d, r)
				b.Get("/items/{id:[0-9]*}", okGet)
				adopt(t, b)
				router = r
			} else {
				r := mux.NewRouter()
				specout.Gorilla(d, r).Get("/items/{id:[0-9]*}", okGet)
				router = r
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/123", nil))
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Contains(t, docPaths(t, d), "/items/{id}")
		})
	}
}

func TestGorillaHostOnlySubrouterAdopt(t *testing.T) {
	d, r := newGen(), mux.NewRouter()
	sub := r.Host("example.com").Subrouter()
	specout.Gorilla(d, sub).Get("/items", okGet)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/items", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	adopt(t, specout.Gorilla(d, r))
	assert.Contains(t, docPaths(t, d), "/items")
}

func TestClosedNullableAnonymousObjects(t *testing.T) {
	type response struct {
		Detail *struct {
			Name string `json:"name"`
		} `json:"detail"`
		Empty *struct{} `json:"empty"`
	}
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	specout.Document(d, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	p := props(t, buildDoc(t, d), "response")
	for _, name := range []string{"detail", "empty"} {
		s := p[name].(map[string]any)
		assert.True(t, isNullable(s))
		assert.Equal(t, false, s["additionalProperties"])
	}
}

func TestShadowedSchemaFieldsFollowJSONDominance(t *testing.T) {
	type Base struct {
		Name *int `json:"name" jsonschema:"readonly"`
	}
	type first struct {
		Base

		Name string `json:"name,omitempty"`
	}
	type last struct {
		Name string `json:"name,omitempty"`
		Base        //nolint:embeddedstructfieldcheck // Regression: the later embedding must not overwrite Name.
	}
	type Left struct{ Name string }
	type Right struct{ Name int }
	type ambiguous struct {
		Left
		Right
	}
	for name, value := range map[string]any{"first": first{}, "last": last{}, "ambiguous": ambiguous{}} {
		t.Run(name, func(t *testing.T) {
			doc := docOf(t, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: 200, Type: value}))
			s := componentsSchema(t, doc, name)
			p := s["properties"].(map[string]any)
			propertyName := "name"
			if name == "ambiguous" {
				propertyName = "Name"
				assert.NotContains(t, p, propertyName)
			} else {
				assert.Equal(t, map[string]any{"type": "string"}, p["name"])
			}
			required, _ := s["required"].([]any)
			assert.NotContains(t, required, propertyName)
		})
	}
}

func TestRepeatedAnonymousFieldsKeepFixups(t *testing.T) {
	type response struct {
		A struct {
			Value *string `json:"value" jsonschema:"readonly"`
		} `json:"a"`
		B struct {
			Value *string `json:"value" jsonschema:"readonly"`
		} `json:"b"`
	}
	p := props(t, docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop}), "response")
	for _, name := range []string{"a", "b"} {
		value := p[name].(map[string]any)["properties"].(map[string]any)["value"].(map[string]any)
		assert.True(t, isNullable(value), name)
		assert.Equal(t, true, value["readOnly"], name)
	}
}

func TestOmittedRoutesDoNotReserveOperationIDs(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/files/{path...}", okGet.WithOperationID("same"))
	specout.Document(d, http.MethodGet, "/items", okGet.WithOperationID("same"))
	assert.Equal(t, []string{"/items"}, keys(docPaths(t, d)))
}

func TestDefaultResponseWithCoverageStatusAllowsOtherCodes(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Key: "default", Status: 404}))
	key := specout.NewRouteKey(http.MethodGet, "/x")
	assert.Equal(t, map[int]bool{404: true}, declaredStatuses(t, d)[key])
	assert.True(t, specStatuses(t, d)[key][0])
	assert.Contains(t, opOf(t, buildDoc(t, d), "/x", "get")["responses"], "default")
}

func TestUploadMediaTypeFollowsVisibleFields(t *testing.T) {
	type Upload struct {
		File specout.File `json:"file"`
	}
	type embedded struct{ Upload }
	type pointer struct {
		File *specout.File `json:"file"`
	}
	type hidden struct {
		File specout.File `json:"-"`
		Name string       `json:"name"`
	}
	for name, doc := range map[string]map[string]any{
		"embedded": docOf(t, http.MethodPost, "/x", specout.Handler[embedded, specout.NoContent]{HandlerFunc: noop}),
		"pointer":  docOf(t, http.MethodPost, "/x", specout.Handler[pointer, specout.NoContent]{HandlerFunc: noop}),
		"hidden":   docOf(t, http.MethodPost, "/x", specout.Handler[hidden, specout.NoContent]{HandlerFunc: noop}),
	} {
		content := opOf(t, doc, "/x", "post")["requestBody"].(map[string]any)["content"].(map[string]any)
		want := "multipart/form-data"
		if name == "hidden" {
			want = "application/json"
		}
		assert.Contains(t, content, want, name)
	}
}

func TestRecursiveCollectionsUseComponents(t *testing.T) {
	type recursiveMap map[string]recursiveMap
	type recursiveSlice []recursiveSlice
	for name, doc := range map[string]map[string]any{
		"recursiveMap":   docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, recursiveMap]{HandlerFunc: noop}),
		"recursiveSlice": docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, recursiveSlice]{HandlerFunc: noop}),
	} {
		s := componentsSchema(t, doc, name)
		key := "additionalProperties"
		if name == "recursiveSlice" {
			key = "items"
		}
		assert.Equal(t, "#/components/schemas/"+name, s[key].(map[string]any)["$ref"])
	}
}

func TestNamedScalarCollectionKeepsSchemaName(t *testing.T) {
	type labels []string
	d := newGen()
	d.SchemaName[labels]("Labels")
	specout.Document(d, http.MethodGet, "/x", specout.Handler[struct{}, labels]{HandlerFunc: noop})
	s := componentsSchema(t, buildDoc(t, d), "Labels")
	assert.Equal(t, "array", s["type"])
	assert.Equal(t, map[string]any{"type": "string"}, s["items"])
}

type integerMapItem struct {
	Value *string `json:"value" jsonschema:"readonly"`
}

type integerMapResponse struct {
	Values map[int]*integerMapItem `json:"values"`
}

func TestSignedMapValuesKeepFixups(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, integerMapResponse]{HandlerFunc: noop})
	values := props(t, doc, "integerMapResponse")["values"].(map[string]any)
	patterns := values["patternProperties"].(map[string]any)
	require.Contains(t, patterns, "^-?[0-9]+$")
	assert.True(t, isNullable(patterns["^-?[0-9]+$"].(map[string]any)))
	value := props(t, doc, "integerMapItem")["value"].(map[string]any)
	assert.True(t, isNullable(value))
	assert.Equal(t, true, value["readOnly"])
}

func TestSignedMapKeysInInlineSchemas(t *testing.T) {
	type request struct {
		Filter map[int]string `jsonschema:"style=deepObject" query:"filter"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, map[int]string]{HandlerFunc: noop})
	op := opOf(t, doc, "/x", "get")
	parameter := paramsOf(t, op)["filter"]["schema"].(map[string]any)
	response := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	for _, s := range []map[string]any{parameter, response} {
		assert.Contains(t, s["patternProperties"], "^-?[0-9]+$")
	}
}

func TestPathOptOutRemainsInBody(t *testing.T) {
	type request struct {
		Name string `json:"name" path:"-"`
		Q    string `query:"q"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	assert.Equal(t, map[string]any{"name": map[string]any{"type": "string"}}, props(t, doc, "requestBody"))
	assert.Len(t, paramsOf(t, opOf(t, doc, "/x", "post")), 1)
}

func TestInvalidResponseKeyStatusCombinationsFail(t *testing.T) {
	for _, response := range []specout.Response{
		{Key: "4XX", Status: 201},
		{Key: "default", Status: 700},
		{Key: "5XX", Status: -1},
	} {
		assert.Panics(t, func() {
			docOf(t, http.MethodGet, "/x", okGet.WithResponse(response))
		})
	}
}

type nullableUnionHolder struct {
	Event *struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
	} `json:"event"`
}

func TestNullableAnonymousUnion(t *testing.T) {
	for _, closed := range []bool{false, true} {
		d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: closed})
		d.Register[unionEmail]("email")
		d.Register[unionSlack]("slack")
		specout.Document(d, http.MethodGet, "/x", specout.Handler[struct{}, nullableUnionHolder]{HandlerFunc: noop})
		event := props(t, buildDoc(t, d), "nullableUnionHolder")["event"].(map[string]any)
		assert.Contains(t, event["oneOf"], map[string]any{"type": "null"})
		assert.NotContains(t, event, "type")
	}
}

type recursiveParameter struct {
	Value string              `json:"value"`
	Next  *recursiveParameter `json:"next,omitempty"`
}

func TestRecursiveParameterUsesResolvableComponents(t *testing.T) {
	type tree map[string]tree
	type nodes []nodes
	type request struct {
		Filter recursiveParameter `jsonschema:"style=deepObject" query:"filter"`
		Tree   tree               `jsonschema:"style=deepObject" query:"tree"`
		Nodes  nodes              `query:"nodes"`
	}
	d := newGen()
	d.SchemaName[recursiveParameter]("FilterNode")
	specout.Document(d, http.MethodGet, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	for _, ref := range componentRefs(doc) {
		assert.Contains(t, schemas(t, doc), strings.TrimPrefix(ref, "#/components/schemas/"))
	}
	assert.True(t, isNullable(props(t, doc, "FilterNode")["next"].(map[string]any)))
	assert.Equal(t, "#/components/schemas/tree", componentsSchema(t, doc, "tree")["additionalProperties"].(map[string]any)["$ref"])
	assert.Equal(t, "#/components/schemas/nodes", componentsSchema(t, doc, "nodes")["items"].(map[string]any)["$ref"])
}

func TestInlineParameterPreservesFieldsAndTags(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	type items []item
	type hidden struct {
		Value int `json:"value"`
	}
	type filter struct {
		Value  string  `json:"value"`
		Mode   *string `json:"mode"  jsonschema:"enum=a|b"`
		Items  items   `json:"items" jsonschema:"minItems=1"`
		hidden         //nolint:embeddedstructfieldcheck // The hidden field must not replace the direct field.
	}
	type limit int
	type labels []string
	type request struct {
		Filter filter `jsonschema:"style=deepObject"    query:"filter"`
		Limit  limit  `jsonschema:"minimum=1,default=2" query:"limit"`
		Labels labels `jsonschema:"minItems=1,enum=a|b" query:"labels"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	parameters := paramsOf(t, opOf(t, doc, "/x", "get"))
	p := parameters["filter"]["schema"].(map[string]any)["properties"].(map[string]any)
	assert.Equal(t, "string", p["value"].(map[string]any)["type"])
	assert.Equal(t, []any{"a", "b", nil}, p["mode"].(map[string]any)["enum"])
	assert.InDelta(t, 1, p["items"].(map[string]any)["minItems"], 0)
	limitSchema := parameters["limit"]["schema"].(map[string]any)
	assert.InDelta(t, 1, limitSchema["minimum"], 0)
	assert.InDelta(t, 2, limitSchema["default"], 0)
	labelsSchema := parameters["labels"]["schema"].(map[string]any)
	assert.InDelta(t, 1, labelsSchema["minItems"], 0)
	assert.Equal(t, []any{"a", "b"}, labelsSchema["items"].(map[string]any)["enum"])
}

type swappedFormFields struct {
	A string `form:"b"          json:"a"`
	B *int   `form:"a"          json:"b,omitempty"`
	C string `form:",omitempty" json:"c"`
}

func TestFormRenamesPreserveSharedFields(t *testing.T) {
	type first struct {
		Value swappedFormFields `json:"value"`
	}
	type second struct {
		Value swappedFormFields `json:"value"`
	}
	d := newGen()
	specout.Document(d, http.MethodPost, "/a", specout.Handler[first, specout.NoContent]{HandlerFunc: noop})
	specout.Document(d, http.MethodPost, "/b", specout.Handler[second, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	p := props(t, doc, "swappedFormFields")
	require.Len(t, p, 3)
	assert.Equal(t, "string", p["b"].(map[string]any)["type"])
	assert.True(t, isNullable(p["a"].(map[string]any)))
	assert.Equal(t, "string", p["c"].(map[string]any)["type"])
	assert.ElementsMatch(t, []any{"b", "c"}, componentsSchema(t, doc, "swappedFormFields")["required"])
}

func TestDuplicateFormNamesFail(t *testing.T) {
	type request struct {
		A string `form:"value" json:"a"`
		B int    `form:"value" json:"b"`
	}
	assert.PanicsWithValue(t, "specout: duplicate form field value", func() {
		docOf(t, http.MethodPost, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	})
}

func TestParameterNamesAreNotOptionalTagOptions(t *testing.T) {
	type request struct {
		EmptyName string `query:"omitempty"`
		ZeroName  string `query:"omitzero"`
		JSONName  string `json:"omitempty"           query:"named"`
		Optional  string `query:"optional,omitempty"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	parameters := paramsOf(t, opOf(t, doc, "/x", "get"))
	for _, name := range []string{"omitempty", "omitzero", "named"} {
		assert.Equal(t, true, parameters[name]["required"], name)
	}
	assert.Equal(t, false, parameters["optional"]["required"])
}

func TestSchemaExcludedBinaryFieldsDoNotSelectMultipart(t *testing.T) {
	type request struct {
		Name   string       `json:"name"`
		File   specout.File `json:"file"   jsonschema:"-"`
		Hidden string       `json:"hidden" jsonschema:"-,format=binary"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	content := opOf(t, doc, "/x", "post")["requestBody"].(map[string]any)["content"].(map[string]any)
	assert.Contains(t, content, "application/json")
	assert.NotContains(t, content, "multipart/form-data")
	assert.Equal(t, map[string]any{"name": map[string]any{"type": "string"}}, props(t, doc, "request"))
}

func TestSchemaNameRejectsInvalidComponentNames(t *testing.T) {
	for _, name := range []string{"", "with space", "has/slash", "has~tilde", "has#fragment"} {
		t.Run(name, func(t *testing.T) {
			d := newGen()
			assert.Panics(t, func() { d.SchemaName[recursiveParameter](name) })
		})
	}
}

func TestSchemaNamePointerOverridesAreDeterministic(t *testing.T) {
	for range 32 {
		d := newGen()
		d.SchemaName[recursiveParameter]("Previous")
		d.SchemaName[*recursiveParameter]("Current")
		specout.Document(d, http.MethodGet, "/x", specout.Handler[struct{}, recursiveParameter]{HandlerFunc: noop})
		doc := buildDoc(t, d)
		assert.Contains(t, schemas(t, doc), "Current")
		assert.NotContains(t, schemas(t, doc), "Previous")
	}
}
