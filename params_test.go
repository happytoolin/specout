package specout_test

import (
	json "encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExcludedParameter(t *testing.T) {
	type request struct {
		Value string `jsonschema:"-,description=Hidden" query:"value"`
	}
	assert.PanicsWithValue(t, "specout: GET /x: field Value: this parameter has no schema; remove jsonschema:\"-\"", func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	})
}

func TestParamKeywords(t *testing.T) {
	type ListReq struct {
		Limit int    `jsonschema:"default=20,minimum=1,maximum=100"     query:"limit"`
		Sort  string `jsonschema:"enum=created|updated,default=created" query:"sort"`
	}
	doc := docOf(t, "GET", "/items", specout.Handler[ListReq, specout.NoContent]{HandlerFunc: noop})
	p := paramsOf(t, opOf(t, doc, "/items", "get"))

	limit := p["limit"]["schema"].(map[string]any)
	assert.InEpsilon(t, 1, limit["minimum"], 0.0001, "limit minimum")
	assert.InEpsilon(t, 100, limit["maximum"], 0.0001, "limit maximum")
	assert.InEpsilon(t, 20, limit["default"], 0.0001, "limit default")
	assert.Equal(t, []any{"created", "updated"}, p["sort"]["schema"].(map[string]any)["enum"], "sort enum")
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

func TestPathOptOutRemainsInBody(t *testing.T) {
	type request struct {
		Name string `json:"name" path:"-"`
		Q    string `query:"q"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	assert.Equal(t, map[string]any{"name": map[string]any{"type": "string"}}, props(t, doc, "requestBody"))
	assert.Len(t, paramsOf(t, opOf(t, doc, "/x", "post")), 1)
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
	d.Document(http.MethodGet, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
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
	d := newGen()
	handler := specout.Handler[swappedFormFields, swappedFormFields]{HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{"application/json", "application/x-www-form-urlencoded"}}}
	d.Document(http.MethodPost, "/a", handler)
	d.Document(http.MethodPost, "/b", handler)
	doc := buildDoc(t, d)
	for _, path := range []string{"/a", "/b"} {
		content := opOf(t, doc, path, "post")["requestBody"].(map[string]any)["content"].(map[string]any)
		form := content["application/x-www-form-urlencoded"].(map[string]any)["schema"].(map[string]any)
		p := form["properties"].(map[string]any)
		require.Len(t, p, 3)
		assert.Equal(t, "string", p["b"].(map[string]any)["type"])
		assert.True(t, isNullable(p["a"].(map[string]any)))
		assert.Equal(t, "string", p["c"].(map[string]any)["type"])
		assert.ElementsMatch(t, []any{"b", "c"}, form["required"])
		assert.Equal(t, "#/components/schemas/swappedFormFields", content["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"])
	}
	jsonProps := props(t, doc, "swappedFormFields")
	assert.Equal(t, "string", jsonProps["a"].(map[string]any)["type"])
	assert.True(t, isNullable(jsonProps["b"].(map[string]any)))
	assert.ElementsMatch(t, []any{"a", "c"}, componentsSchema(t, doc, "swappedFormFields")["required"])
}

func TestDuplicateFormNamesFail(t *testing.T) {
	type request struct {
		A string `form:"value" json:"a"`
		B int    `form:"value" json:"b"`
	}
	assert.PanicsWithValue(t, "specout: POST /x: two fields have the form name \"value\"; change one form tag", func() {
		docOf(t, http.MethodPost, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{"application/x-www-form-urlencoded"}}})
	})
}

func TestFormDashFieldLeavesTheFormSchema(t *testing.T) {
	type request struct {
		Name   string `form:"name" json:"name"`
		Secret string `form:"-"    json:"secret"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{"application/json", "application/x-www-form-urlencoded"}}})
	content := opOf(t, doc, "/x", "post")["requestBody"].(map[string]any)["content"].(map[string]any)
	form := content["application/x-www-form-urlencoded"].(map[string]any)["schema"].(map[string]any)
	assert.Equal(t, []string{"name"}, keys(form["properties"].(map[string]any)))
	assert.Equal(t, []any{"name"}, form["required"])
	assert.Contains(t, props(t, doc, "request"), "secret", "the JSON body keeps the field")
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

func TestSplitBodyTagErrorNamesTheUserType(t *testing.T) {
	type mixed struct {
		ID   string `path:"id"`
		Name string `json:"name" jsonschema:"minLenght=1"`
	}
	msg := panicMessage(t, func() {
		docOf(t, http.MethodPost, "/x/{id}", specout.Handler[mixed, job]{HandlerFunc: noop})
	})
	assert.Contains(t, msg, `specout_test.mixed field Name: jsonschema keyword "minLenght" is not known`)
	assert.NotContains(t, msg, "Field1")
}

func TestRequestExamplesMatchTheBodySchema(t *testing.T) {
	type lamp struct {
		ID   string `path:"id"`
		Q    string `query:"q"`
		Name string `json:"name"`
	}
	doc := docOf(t, http.MethodPost, "/items/{id}", specout.Handler[lamp, job]{
		HandlerFunc: noop, Request: specout.Request{Examples: map[string]specout.Example{"lamp": {Value: lamp{ID: "1", Q: "z", Name: "Lamp"}}}},
	})
	media := child(t, opOf(t, doc, "/items/{id}", "post"), "requestBody", "content", "application/json")
	assert.Equal(t, map[string]any{"name": "Lamp"}, child(t, media, "examples", "lamp")["value"])

	type upload struct {
		Name   string       `form:"display_name" json:"name"`
		Secret string       `form:"-"            json:"secret"`
		Avatar specout.File `json:"avatar"`
	}
	value := upload{Name: "Lamp", Secret: "s"}
	doc = docOf(t, http.MethodPost, "/upload", specout.Handler[upload, job]{
		HandlerFunc: noop, Request: specout.Request{
			ContentTypes: []string{"multipart/form-data", "multipart/mixed"},
			Examples:     map[string]specout.Example{"one": {Value: value}},
		},
	})
	content := child(t, opOf(t, doc, "/upload", "post"), "requestBody", "content")
	assert.Equal(t, map[string]any{"display_name": "Lamp"}, child(t, content, "multipart/form-data", "examples", "one")["value"])
	assert.Equal(t, map[string]any{"name": "Lamp", "secret": "s"}, child(t, content, "multipart/mixed", "examples", "one")["value"])

	assert.Contains(t, panicMessage(t, func() {
		newGen().Document(http.MethodPost, "/x", specout.Handler[job, job]{
			HandlerFunc: noop, Request: specout.Request{Examples: map[string]specout.Example{"a": {Summary: "nothing"}}},
		})
	}), `example "a" has no value; set Value or ExternalValue`)
}

func TestEncodingFollowsTheMediaType(t *testing.T) {
	type note struct {
		Name string `form:"title" json:"name"`
	}
	form := "application/x-www-form-urlencoded; charset=utf-8"
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[note, job]{HandlerFunc: noop, Request: specout.Request{
		ContentTypes: []string{form}, Encoding: map[string]specout.Encoding{"title": {Style: "form"}},
	}})
	media := child(t, opOf(t, doc, "/x", "post"), "requestBody", "content", form)
	assert.Contains(t, child(t, media, "encoding"), "title")

	mixed := func(name string) func() {
		return func() {
			docOf(t, http.MethodPost, "/x", specout.Handler[note, job]{HandlerFunc: noop, Request: specout.Request{
				ContentTypes: []string{"multipart/mixed"}, Encoding: map[string]specout.Encoding{name: {ContentType: "text/plain"}},
			}})
		}
	}
	assert.NotPanics(t, mixed("name"))
	assert.Contains(t, panicMessage(t, mixed("title")), `"title": the multipart/mixed body has no such field, only name`)
}

func TestEscapedCommasDoNotSetFlags(t *testing.T) {
	type flags struct {
		Q    string `jsonschema:"description=Not\\,deprecated" query:"q"`
		Name string `json:"name"                               jsonschema:"description=Not\\,deprecated"`
		Size int    `json:"size"                               jsonschema:"description=Use\\,enum=1|2"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[flags, job]{HandlerFunc: noop})
	q := paramsOf(t, opOf(t, doc, "/x", "post"))["q"]
	assert.NotContains(t, q, "deprecated")
	assert.Equal(t, "Not,deprecated", q["description"])
	properties := componentsSchema(t, doc, "flagsBody")["properties"].(map[string]any)
	assert.NotContains(t, properties["name"], "deprecated")
	assert.NotContains(t, properties["size"], "enum")
}

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
	type duplicateHeader struct {
		A string `header:"X-Id"`
		B string `header:"x-id"`
	}
	assert.PanicsWithValue(t, "specout: GET /x: field B: query parameter name \"q\" is empty or repeated", func() { docOf(t, "GET", "/x", specout.Handler[duplicate, string]{HandlerFunc: noop}) })
	assert.PanicsWithValue(t, "specout: GET /x: field B: header parameter name \"x-id\" is empty or repeated", func() { docOf(t, "GET", "/x", specout.Handler[duplicateHeader, string]{HandlerFunc: noop}) })
	assert.PanicsWithValue(t, "specout: GET /x: field A: style \"deepObject\" is not valid on a header parameter; use simple", func() { docOf(t, "GET", "/x", specout.Handler[badStyle, string]{HandlerFunc: noop}) })
}

func TestParameterTagsKeepEscapedCommas(t *testing.T) {
	type request struct {
		Q string `jsonschema:"description=Page size\\, at most 100,style=form" query:"q"`
		B string `json:"b"                                                     jsonschema:"description=Body text\\, plain"`
	}
	doc := docOf(t, "POST", "/x", specout.Handler[request, string]{HandlerFunc: noop})
	q := paramsOf(t, opOf(t, doc, "/x", "post"))["q"]
	assert.Equal(t, "Page size, at most 100", q["description"])
	assert.Equal(t, "form", q["style"])
	assert.Equal(t, "Body text, plain", props(t, doc, "requestBody")["b"].(map[string]any)["description"])
}

type patchReq struct {
	PetID int64    `jsonschema:"format=int64" path:"petId"`
	Dry   bool     `query:"dry_run"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags,omitempty"`
}

type onlyParams struct {
	ID   int64  `path:"id"`
	Sort string `query:"sort"`
}

type tagBody struct {
	Name string `json:"name"`
}

func requestBody(t *testing.T, op map[string]any) (string, map[string]any) {
	t.Helper()
	rb, ok := op["requestBody"].(map[string]any)
	require.True(t, ok, "no requestBody in %v", op)
	content, ok := rb["content"].(map[string]any)
	require.True(t, ok, "no content in %v", rb)
	require.Len(t, content, 1, "content types = %v, want one", content)
	for ct, raw := range content {
		return ct, raw.(map[string]any)
	}
	return "", nil
}

func TestBodyExcludesParamFields(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("PATCH", "/pets/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: okBody})
	specout.Bind(d, specoutstd.New(r)).Handle("POST", "/pets/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)

	item := doc["paths"].(map[string]any)["/pets/{petId}"].(map[string]any)
	op := item["patch"].(map[string]any)
	params := paramsOf(t, op)
	assert.Len(t, params, 2)
	assert.Equal(t, "path", params["petId"]["in"])
	assert.Equal(t, "query", params["dry_run"]["in"])

	ct, media := requestBody(t, op)
	assert.Equal(t, "application/json", ct)
	schema := media["schema"].(map[string]any)
	assert.Equal(t, "#/components/schemas/patchReqBody", schema["$ref"], "body schema")

	body := schemas(t, doc)["patchReqBody"].(map[string]any)
	bProps := body["properties"].(map[string]any)
	assert.Len(t, bProps, 2, "body properties = %v, want name+tags only", bProps)
	assert.Contains(t, bProps, "name")
	assert.Contains(t, bProps, "tags")
	assert.Equal(t, []any{"name"}, body["required"], "body required")

	_, postMedia := requestBody(t, item["post"].(map[string]any))
	assert.Equal(t, schema["$ref"], postMedia["schema"].(map[string]any)["$ref"])
}

func TestAllParamReqHasNoBody(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("POST", "/things/{id}", specout.Handler[onlyParams, specout.NoContent]{HandlerFunc: okBody})
	doc := buildDoc(t, d)
	assert.NotContains(t, opOf(t, doc, "/things/{id}", "post"), "requestBody")
}

func TestNonStructRequestBody(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("POST", "/tags", specout.Handler[[]tagBody, specout.NoContent]{HandlerFunc: okBody})
	specout.Bind(d, specoutstd.New(r)).Handle("POST", "/upload", specout.Handler[specout.File, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)

	ct, media := requestBody(t, opOf(t, doc, "/tags", "post"))
	assert.Equal(t, "application/json", ct)
	assert.Equal(t, "#/components/schemas/tagBodyList", media["schema"].(map[string]any)["$ref"])

	ct, media = requestBody(t, opOf(t, doc, "/upload", "post"))
	assert.Equal(t, "application/octet-stream", ct)
	schema := media["schema"].(map[string]any)
	assert.Equal(t, "string", schema["type"])
	assert.Equal(t, "binary", schema["format"])
	assert.NotContains(t, schemas(t, doc), "File")
}

func TestBodySchemaJSONShape(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("POST", "/mixed/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: okBody})
	doc := buildDoc(t, d)
	raw, err := json.Marshal(schemas(t, doc)["patchReqBody"])
	require.NoError(t, err)
	got := string(raw)
	assert.Contains(t, got, `"name"`)
	assert.NotContains(t, got, "petId")
	assert.NotContains(t, got, "PetID")
}

type bodyClash struct {
	ID   string `path:"id"`
	Name string `json:"name"`
}

type bodyClashBody struct {
	X int `json:"x"`
}

func TestBodyNameClashShowsEachTagOnce(t *testing.T) {
	msg, _ := panicValue(func() {
		docOf(t, http.MethodPost, "/x/{id}", specout.Handler[bodyClash, bodyClashBody]{HandlerFunc: noop})
	}).(string)
	assert.Contains(t, msg, `duplicate component name "bodyClashBody"`)
	assert.Equal(t, 1, strings.Count(msg, `json:\"name\"`), msg)
}
