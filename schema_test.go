package specout_test

import (
	"context"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
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
	d.Document(http.MethodGet, "/a", specout.Handler[struct{}, schemaSharedA]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/b", specout.Handler[struct{}, schemaSharedB]{HandlerFunc: noop})
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
	d.Document(http.MethodGet, "/recursive", specout.Handler[struct{}, RecursiveEmbedded]{HandlerFunc: noop})
	assert.PanicsWithValue(t, "specout: GET /recursive: recursive anonymous embedding in specout_test.RecursiveEmbedded", func() {
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
	d.Document(http.MethodPost, "/maps", specout.Handler[schemaMapBody, specout.NoContent]{HandlerFunc: noop})
	p := props(t, buildDoc(t, d), "schemaMapBody")

	typed := p["typed"].(map[string]any)
	assert.Equal(t, "string", typed["additionalProperties"].(map[string]any)["type"])
	assert.NotEqual(t, false, p["free"].(map[string]any)["additionalProperties"])
}

type schemaHiddenEmbed struct {
	Secret string `json:"secret" jsonschema:"-"`
}

func TestClosedSchemasRejectHiddenFields(t *testing.T) {
	type direct struct {
		Name   string `json:"name"`
		Secret string `json:"secret" jsonschema:"-"`
	}
	type embedded struct {
		schemaHiddenEmbed

		Name string `json:"name"`
	}
	type hiddenEmbed struct {
		schemaHiddenEmbed `jsonschema:"-"`

		Name string `json:"name"`
	}
	type ignored struct {
		Name   string `json:"name"`
		Secret string `json:"-"    jsonschema:"-"`
	}
	for want, body := range map[string]any{
		"direct field Secret":                 direct{},
		"embedded field Secret":               embedded{},
		"hiddenEmbed field schemaHiddenEmbed": hiddenEmbed{},
	} {
		t.Run(want, func(t *testing.T) {
			d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
			d.Document(http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: body}))
			assert.PanicsWithValue(t, "specout: GET /x: specout_test."+want+` has jsonschema:"-", but JSON still writes it, `+
				`and ClosedSchemas rejects it; tag it json:"-", or drop ClosedSchemas`, func() { _, _ = d.Build() })
		})
	}
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Document(http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: ignored{}}))
	assert.NotContains(t, props(t, buildDoc(t, d), "ignored"), "secret")
	open := newGen()
	open.Document(http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: direct{}}))
	assert.NotContains(t, props(t, buildDoc(t, open), "direct"), "secret", "an open schema keeps the hidden field out")
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
	d.Document(http.MethodPost, "/requests/{id}", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/requests", specout.Handler[struct{}, envelope]{HandlerFunc: noop})
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

func TestNumberEnumTagsSplitOnPipes(t *testing.T) {
	type response struct {
		Code  int      `json:"code"  jsonschema:"enum=1|2"`
		Mixed int      `json:"mixed" jsonschema:"enum=1|2,enum=3"`
		Ratio float64  `json:"ratio" jsonschema:"enum=0.5|1.5"`
		Ptr   *uint    `json:"ptr"   jsonschema:"enum=4|5"`
		List  []int    `json:"list"  jsonschema:"enum=6|7"`
		Null  *float32 `json:"null"  jsonschema:"nullable,enum=8|9"`
	}
	type request struct {
		Level int `jsonschema:"enum=1|2" query:"level"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, response]{HandlerFunc: noop})
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"listed numbers", schema, response{Code: 2, Mixed: 1, Ratio: 1.5, List: []int{7}}, true},
		payloadCase{"unlisted number", schema, response{Code: 3, Mixed: 3, Ratio: 0.5}, false},
	)
	p := props(t, doc, "response")
	assert.Equal(t, []any{1.0, 2.0}, p["code"].(map[string]any)["enum"])
	assert.Equal(t, []any{1.0, 2.0, 3.0}, p["mixed"].(map[string]any)["enum"])
	assert.Equal(t, []any{0.5, 1.5}, p["ratio"].(map[string]any)["enum"])
	assert.Equal(t, []any{4.0, 5.0, nil}, p["ptr"].(map[string]any)["enum"])
	assert.Equal(t, []any{6.0, 7.0}, p["list"].(map[string]any)["items"].(map[string]any)["enum"])
	assert.Equal(t, []any{8.0, 9.0}, p["null"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)["enum"])
	parameter := paramsOf(t, opOf(t, doc, "/x", "get"))["level"]
	assert.Equal(t, []any{1.0, 2.0}, parameter["schema"].(map[string]any)["enum"])
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

type schemaTextID struct{ hi, lo uint64 }

func (id schemaTextID) MarshalText() ([]byte, error) {
	if id == (schemaTextID{}) {
		return nil, errors.New("empty ID")
	}
	return []byte(strconv.FormatUint(id.hi, 16) + "-" + strconv.FormatUint(id.lo, 16)), nil
}

type schemaTextKey int

func (k schemaTextKey) MarshalText() ([]byte, error) {
	if k < 0 {
		return nil, errors.New("negative key")
	}
	return []byte("key-" + strconv.Itoa(int(k))), nil
}

func TestTextMarshalersAreStrings(t *testing.T) {
	type response struct {
		ID   schemaTextID          `json:"id"`
		Ptr  *schemaTextID         `json:"ptr"`
		Addr netip.Addr            `json:"addr"`
		Keys map[schemaTextKey]int `json:"keys"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/id", specout.Handler[struct{}, schemaTextID]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	value := response{ID: schemaTextID{1, 2}, Addr: netip.MustParseAddr("::1"), Keys: map[schemaTextKey]int{5: 1}}
	exportPayloadCases(t, doc,
		payloadCase{"text values", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{"text root", responseSchemaOf(t, doc, "/id", "get"), schemaTextID{3, 4}, true},
		payloadCase{"object for a text value", responseSchemaOf(t, doc, "/id", "get"), map[string]any{}, false},
	)
	p := props(t, doc, "response")
	assert.Equal(t, "string", p["id"].(map[string]any)["type"])
	assert.Equal(t, []any{"string", "null"}, p["ptr"].(map[string]any)["type"])
	assert.Equal(t, "string", p["addr"].(map[string]any)["type"])
	assert.NotContains(t, p["keys"], "patternProperties", "MarshalText writes the keys, not the integers")
	assert.Equal(t, "string", componentsSchema(t, doc, "schemaTextID")["type"])
}

func TestUintptrIsAnInteger(t *testing.T) {
	type response struct {
		Address uintptr `json:"address"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/root", specout.Handler[struct{}, uintptr]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	exportPayloadCases(t, doc,
		payloadCase{"uintptr field", responseSchemaOf(t, doc, "/x", "get"), response{Address: 42}, true},
		payloadCase{"uintptr root", responseSchemaOf(t, doc, "/root", "get"), uintptr(7), true},
		payloadCase{"string for uintptr", responseSchemaOf(t, doc, "/root", "get"), "7", false},
	)
	assert.Equal(t, "integer", props(t, doc, "response")["address"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "integer"}, responseSchemaOf(t, doc, "/root", "get"))
}

type schemaStamp struct {
	time.Time

	Note string `json:"note"`
}

type schemaOwnStamp struct {
	time.Time

	Note string `json:"note"`
}

func (s schemaOwnStamp) MarshalJSON() ([]byte, error) {
	if s.Note == "" {
		return nil, errors.New("empty note")
	}
	return []byte(strconv.Quote(s.Note)), nil
}

func TestPromotedEncoderSetsTheSchema(t *testing.T) {
	type response struct {
		At   schemaStamp   `json:"at"`
		Ptr  *schemaStamp  `json:"ptr"`
		List []schemaStamp `json:"list"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/stamp", specout.Handler[struct{}, schemaStamp]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/own", specout.Handler[struct{}, schemaOwnStamp]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	stamp := schemaStamp{Time: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC), Note: "dropped"}
	wire, err := jsonv1.Marshal(stamp)
	require.NoError(t, err)
	require.JSONEq(t, `"2026-10-29T12:00:00Z"`, string(wire), "encoding/json writes the embedded time")
	value := response{At: stamp, List: []schemaStamp{stamp}}
	exportPayloadCases(t, doc,
		payloadCase{"promoted encoder", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{"promoted root", responseSchemaOf(t, doc, "/stamp", "get"), stamp, true},
		payloadCase{
			"fields for a promoted encoder", responseSchemaOf(t, doc, "/stamp", "get"),
			map[string]any{"note": "x"},
			false,
		},
	)
	timeSchema := map[string]any{"type": "string", "format": "date-time"}
	p := props(t, doc, "response")
	assert.Equal(t, timeSchema, p["at"])
	assert.Equal(t, timeSchema, p["list"].(map[string]any)["items"])
	assert.Equal(t, []any{"string", "null"}, p["ptr"].(map[string]any)["type"])
	assert.Equal(t, timeSchema, componentsSchema(t, doc, "schemaStamp"))
	assert.Contains(t, props(t, doc, "schemaOwnStamp"), "note", "a declared MarshalJSON keeps the reflected fields")
}

type schemaInnerStamp struct{ time.Time }

type schemaOuterStamp struct {
	time.Time
	schemaInnerStamp
}

func TestShallowestPromotedEncoderSetsTheSchema(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, schemaOuterStamp]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	value := schemaOuterStamp{Time: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	wire, err := jsonv1.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `"2026-10-29T12:00:00Z"`, string(wire), "encoding/json writes the shallowest time")
	exportPayloadCases(t, doc,
		payloadCase{"shallowest promoted encoder", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{"object for a promoted encoder", responseSchemaOf(t, doc, "/x", "get"), map[string]any{}, false},
	)
	assert.Equal(t, map[string]any{"type": "string", "format": "date-time"}, componentsSchema(t, doc, "schemaOuterStamp"))
}

func TestPointerTextMarshalerIsAString(t *testing.T) {
	type response struct {
		Pattern  *regexp.Regexp  `json:"pattern"`
		Patterns []regexp.Regexp `json:"patterns"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	value := response{Pattern: regexp.MustCompile(`^a+$`), Patterns: []regexp.Regexp{*regexp.MustCompile(`b`)}}
	wire, err := jsonv1.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"pattern":"^a+$","patterns":["b"]}`, string(wire))
	exportPayloadCases(t, doc,
		payloadCase{"pointer text values", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{"null pointer text value", responseSchemaOf(t, doc, "/x", "get"), response{}, true},
		payloadCase{
			"object for a pointer text value", responseSchemaOf(t, doc, "/x", "get"),
			map[string]any{"pattern": map[string]any{}, "patterns": []any{}},
			false,
		},
	)
	p := props(t, doc, "response")
	assert.Equal(t, []any{"string", "null"}, p["pattern"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "string"}, p["patterns"].(map[string]any)["items"])
}

func TestBigNumbers(t *testing.T) {
	type response struct {
		Int   *big.Int   `json:"int"`
		Float *big.Float `json:"float"`
		Rat   *big.Rat   `json:"rat"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/int", specout.Handler[struct{}, *big.Int]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	huge, ok := new(big.Int).SetString("123456789012345678901234567890", 10)
	require.True(t, ok)
	value := response{Int: huge, Float: big.NewFloat(1.5), Rat: big.NewRat(1, 3)}
	wire, err := jsonv1.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"int":123456789012345678901234567890,"float":"1.5","rat":"1/3"}`, string(wire))
	exportPayloadCases(t, doc,
		payloadCase{"big values", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{"null big values", responseSchemaOf(t, doc, "/x", "get"), response{}, true},
		payloadCase{
			"string for big.Int", responseSchemaOf(t, doc, "/x", "get"),
			map[string]any{"int": "1", "float": "1", "rat": "1"},
			false,
		},
	)
	p := props(t, doc, "response")
	assert.Equal(t, []any{"integer", "null"}, p["int"].(map[string]any)["type"])
	assert.Equal(t, []any{"string", "null"}, p["float"].(map[string]any)["type"])
	assert.Equal(t, []any{"string", "null"}, p["rat"].(map[string]any)["type"])
	root := opOf(t, doc, "/int", "get")["responses"].(map[string]any)["200"].(map[string]any)
	assert.Equal(t, map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "null"}}},
		root["content"].(map[string]any)["application/json"].(map[string]any)["schema"])
}

func TestJSONNumber(t *testing.T) {
	type response struct {
		Amount jsonv1.Number `json:"amount"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	value := response{Amount: "12.5"}
	wire, err := jsonv1.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"amount":12.5}`, string(wire))
	exportPayloadCases(t, doc,
		payloadCase{"json.Number", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{
			"string for json.Number", responseSchemaOf(t, doc, "/x", "get"),
			map[string]any{"amount": "12.5"},
			false,
		},
	)
	assert.Equal(t, "number", props(t, doc, "response")["amount"].(map[string]any)["type"])
}

func TestUnencodableTypesPanicClearly(t *testing.T) {
	for want, value := range map[string]any{
		"complex64": struct {
			C complex64 `json:"c"`
		}{},
		"func()": struct {
			F func() `json:"f"`
		}{},
		"chan int": struct {
			L []chan int `json:"l"`
		}{},
	} {
		t.Run(want, func(t *testing.T) {
			assert.PanicsWithValue(t, "specout: GET /x: JSON cannot encode "+want+`; tag its field json:"-"`, func() {
				docOf(t, http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: value}))
			})
		})
	}
	type skipped struct {
		F func() `json:"-"`
		A int    `json:"a"`
	}
	assert.NotPanics(t, func() { docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, skipped]{HandlerFunc: noop}) })
}

type schemaJob struct {
	Name string
	Run  func()
}

func (j schemaJob) MarshalJSON() ([]byte, error) {
	if j.Name == "" {
		return nil, errors.New("empty job")
	}
	return []byte(strconv.Quote(j.Name)), nil
}

func (schemaJob) JSONSchema() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }

type schemaTextJob struct {
	Name string
	Run  chan int
}

func (j schemaTextJob) MarshalText() ([]byte, error) {
	if j.Name == "" {
		return nil, errors.New("empty job")
	}
	return []byte(j.Name), nil
}

type schemaStampJob struct {
	time.Time

	Run func()
}

func TestOwnEncoderHidesUnencodableFields(t *testing.T) {
	type response struct {
		Job   schemaJob      `json:"job"`
		Jobs  []schemaJob    `json:"jobs"`
		Text  schemaTextJob  `json:"text"`
		Stamp schemaStampJob `json:"stamp"`
	}
	var doc map[string]any
	require.NotPanics(t, func() { doc = docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop}) })
	value := response{
		Job: schemaJob{Name: "a"}, Jobs: []schemaJob{{Name: "b"}}, Text: schemaTextJob{Name: "c"},
		Stamp: schemaStampJob{Time: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)},
	}
	wire, err := jsonv1.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"job":"a","jobs":["b"],"text":"c","stamp":"2026-10-29T12:00:00Z"}`, string(wire))
	exportPayloadCases(t, doc,
		payloadCase{"own encoders", responseSchemaOf(t, doc, "/x", "get"), value, true},
		payloadCase{"fields for a text job", responseSchemaOf(t, doc, "/x", "get"), map[string]any{"text": map[string]any{}}, false},
	)
	p := props(t, doc, "response")
	assert.Equal(t, "string", p["text"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "string", "format": "date-time"}, p["stamp"])
}

type (
	dupA struct{ A string }
	dupB struct{ B int }
)

type Contact struct {
	Handle string `json:"handle"`
}

type nestedDup struct {
	Local Contact         `json:"local"`
	Lib   specout.Contact `json:"lib"`
}

func TestDuplicateComponentNamePanics(t *testing.T) {
	d := newGen()
	d.SchemaName[dupA]("Clash")
	d.SchemaName[dupB]("Clash")
	d.Document(http.MethodGet, "/a", specout.Handler[struct{}, dupA]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/b", specout.Handler[struct{}, dupB]{HandlerFunc: noop})
	require.Panics(t, func() { buildDoc(t, d) }, "expected duplicate-name panic")
}

func TestSameNamedNestedTypesPanic(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop})
	require.Panics(t, func() { buildDoc(t, d) }, "expected a duplicate-name panic")
}

func TestNestedOnlyOverrideApplies(t *testing.T) {
	d := newGen()
	d.SchemaName[Contact]("LocalContact")
	r := http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop})
	doc := buildDoc(t, d)

	comps := schemas(t, doc)
	local, ok := comps["LocalContact"].(map[string]any)
	require.True(t, ok, "the override did not apply")
	assert.Contains(t, local["properties"].(map[string]any), "handle", "LocalContact")
	assert.Len(t, comps["Contact"].(map[string]any)["properties"].(map[string]any), 3, "Contact wants the library shape")
	np := props(t, doc, "nestedDup")
	assert.Equal(t, "#/components/schemas/LocalContact", np["local"].(map[string]any)["$ref"], "local ref")
	assert.Equal(t, "#/components/schemas/Contact", np["lib"].(map[string]any)["$ref"], "lib ref")
}

type Anonymous1 struct {
	ID string `json:"id"`
}

func TestAnonymousNameSkipsUserTypeName(t *testing.T) {
	type anonymous = struct {
		Name string `json:"name"`
	}
	for _, userFirst := range []bool{true, false} {
		d := newGen()
		userPath, anonPath := "/a", "/b"
		if !userFirst {
			userPath, anonPath = anonPath, userPath
		}
		d.Document(http.MethodGet, userPath, specout.Handler[struct{}, Anonymous1]{HandlerFunc: noop})
		d.Document(http.MethodGet, anonPath, specout.Handler[struct{}, anonymous]{HandlerFunc: noop})
		doc := buildDoc(t, d)
		assert.Contains(t, props(t, doc, "Anonymous1"), "id", "userFirst=%t", userFirst)
		assert.Contains(t, props(t, doc, "Anonymous2"), "name", "userFirst=%t", userFirst)
		assert.Equal(t, "#/components/schemas/Anonymous2", responseSchemaOf(t, doc, anonPath, "get")["$ref"])
	}
}

func TestRootPointerNullability(t *testing.T) {
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[*listItem, *listItem]{HandlerFunc: noop})
	op := opOf(t, doc, "/x", "post")
	body := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	response := responseSchemaOf(t, doc, "/x", "post")
	exportPayloadCases(t, doc,
		payloadCase{"nil request", body, (*listItem)(nil), true},
		payloadCase{"nil response", response, (*listItem)(nil), true},
		payloadCase{"object response", response, listItem{Name: "valid"}, true},
	)
	assert.True(t, isNullable(body), "pointer request bodies must allow null")
	assert.True(t, isNullable(response), "nil response pointers serialize as null")
}

func TestJSONNamesIgnoreFormTags(t *testing.T) {
	type response struct {
		Name string `form:"display_name" json:"name"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	exportPayloadCases(t, doc, payloadCase{"JSON response", responseSchemaOf(t, doc, "/x", "get"), response{Name: "Alice"}, true})
	assert.Contains(t, props(t, doc, "response"), "name")
	assert.NotContains(t, props(t, doc, "response"), "display_name")
}

func TestNilEmbeddedPointer(t *testing.T) {
	type response struct{ *listItem }
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"nil embedding", schema, response{}, true},
		payloadCase{"populated embedding", schema, response{listItem: &listItem{Name: "valid"}}, true},
	)
	required, _ := componentsSchema(t, doc, "response")["required"].([]any)
	assert.NotContains(t, required, "name")
}

func TestNullableWrapperFixups(t *testing.T) {
	type response struct {
		Child struct {
			Name *string `json:"name"`
		} `json:"child" jsonschema:"nullable"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	exportPayloadCases(t, doc, payloadCase{"nested nil pointer", responseSchemaOf(t, doc, "/x", "get"), response{}, true})
	child := props(t, doc, "response")["child"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)
	assert.True(t, isNullable(child["properties"].(map[string]any)["name"].(map[string]any)))
}

func TestExcludedRecursiveField(t *testing.T) {
	type response struct {
		Hidden recursiveEmbedding `json:"-"`
		Name   string             `json:"name"`
	}
	assert.NotPanics(t, func() {
		doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
		assert.Equal(t, []string{"name"}, keys(props(t, doc, "response")))
	}, "a JSON-excluded field must not prevent schema generation")
}

func TestRepeatedBooleanEnums(t *testing.T) {
	type request struct {
		Enabled bool `jsonschema:"enum=true,enum=false" query:"enabled"`
	}
	type response struct {
		Enabled bool `json:"enabled" jsonschema:"enum=true,enum=false"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, response]{HandlerFunc: noop})
	parameter := paramsOf(t, opOf(t, doc, "/x", "get"))["enabled"]["schema"].(map[string]any)
	exportPayloadCases(t, doc,
		payloadCase{"false response", responseSchemaOf(t, doc, "/x", "get"), response{Enabled: false}, true},
		payloadCase{"false parameter", parameter, false, true},
	)
	assert.ElementsMatch(t, []any{true, false}, props(t, doc, "response")["enabled"].(map[string]any)["enum"])
	assert.ElementsMatch(t, []any{true, false}, parameter["enum"])
}

func TestExcludedEmbeddedSchema(t *testing.T) {
	type response struct {
		listItem `jsonschema:"-"`

		Visible string `json:"visible"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	assert.Equal(t, []string{"visible"}, keys(props(t, doc, "response")))
	assert.NotContains(t, componentsSchema(t, doc, "response")["required"], "name")
}

func documentPointerCycle(t *testing.T, mode string) {
	t.Helper()
	defer func() {
		if caught := recover(); caught != nil {
			assert.True(t, strings.HasPrefix(fmt.Sprint(caught), "specout:"), "unexpected panic: %v", caught)
		}
	}()
	d := newGen()
	if mode == "request" {
		d.Document(http.MethodPost, "/x", specout.Handler[pointerCycle, string]{HandlerFunc: noop})
	} else {
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, pointerCycle]{HandlerFunc: noop})
	}
	if _, err := d.Build(); err != nil {
		t.Logf("recursive type rejected: %v", err)
	}
}

func TestRecursivePointerTerminates(t *testing.T) {
	if mode := os.Getenv("SPECOUT_POINTER_CYCLE"); mode != "" {
		documentPointerCycle(t, mode)
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"request", "response"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestRecursivePointerTerminates$")
			command.Env = append(os.Environ(), "SPECOUT_POINTER_CYCLE="+mode)
			output, runErr := command.CombinedOutput()
			require.NoError(t, ctx.Err(), "schema generation hung for recursive %s pointer: %s", mode, output)
			require.NoError(t, runErr, "%s", output)
		})
	}
}

func TestNullableHelperRejectsOrdinaryUnion(t *testing.T) {
	assert.False(t, isNullable(map[string]any{"oneOf": []any{
		map[string]any{"type": "string"}, map[string]any{"type": "integer"},
	}}))
}

func TestPointerContainersAndSplitBodies(t *testing.T) {
	type request struct {
		*listItem

		Limit int `json:"-" query:"limit"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[*request, *[]*listItem]{HandlerFunc: noop})
	body := opOf(t, doc, "/x", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"]
	response := responseSchemaOf(t, doc, "/x", "post")
	exportPayloadCases(t, doc,
		payloadCase{"nil split body", body, (*request)(nil), true},
		payloadCase{"nil embedded body", body, request{}, true},
		payloadCase{"populated body", body, request{listItem: &listItem{Name: "present"}}, true},
		payloadCase{"nil container pointer", response, (*[]*listItem)(nil), true},
		payloadCase{"nullable array elements", response, []*listItem{nil, {Name: "present"}}, true},
		payloadCase{"wrong array element", response, []any{42}, false},
	)
}

func TestNullableTaggedContainersAndEnums(t *testing.T) {
	type response struct {
		Enabled bool               `json:"enabled" jsonschema:"nullable,enum=true,enum=false"`
		Items   []*listItem        `json:"items"   jsonschema:"nullable"`
		Names   map[string]*string `json:"names"   jsonschema:"nullable"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"nullable tagged values", schema, map[string]any{"enabled": nil, "items": nil, "names": nil}, true},
		payloadCase{"nested nullable container values", schema, response{Items: []*listItem{nil}, Names: map[string]*string{"a": nil}}, true},
		payloadCase{"invalid tagged bool", schema, map[string]any{"enabled": 42, "items": nil, "names": nil}, false},
	)
}

func TestLiteralDashRequiredField(t *testing.T) {
	type response struct {
		Value int `json:"-,string"` //nolint:staticcheck // Test the legacy spelling of a literal dash shared by JSON v1 and v2.
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"literal dash field", schema, response{Value: 42}, true},
		payloadCase{"missing literal dash field", schema, map[string]any{}, false},
	)
	assert.Contains(t, componentsSchema(t, doc, "response")["required"], "-")
}

func TestLiteralDashRequiredTag(t *testing.T) {
	type response struct {
		Value string `json:"-,omitempty" jsonschema:"required"` //nolint:staticcheck // Test the legacy spelling of a literal dash.
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"required tagged dash field", schema, response{Value: "value"}, true},
		payloadCase{"missing required tagged dash field", schema, map[string]any{}, false},
	)
	assert.Contains(t, componentsSchema(t, doc, "response")["required"], "-")
}

func TestIgnoredRecursiveFields(t *testing.T) {
	type response struct {
		SchemaHidden recursiveEmbedding `jsonschema:"-"`
		private      recursiveEmbedding //nolint:unused // The schema walk must ignore unexported fields.
		Name         string             `json:"name"`
	}
	assert.NotPanics(t, func() {
		doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
		assert.Equal(t, []string{"name"}, keys(props(t, doc, "response")))
	})
}

func TestMalformedJSONNames(t *testing.T) {
	for _, name := range []string{"'a,b'", "'-'", "'a b'", "'a\\'b'"} {
		t.Run(name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{Name: "Value", Type: reflect.TypeFor[string](), Tag: reflect.StructTag("json:" + strconv.Quote(name))}})
			value := reflect.New(typ).Elem()
			value.Field(0).SetString("present")
			_, err := json.Marshal(value.Interface())
			require.Error(t, err, "control: these tags are invalid for the JSON encoder")
			field, _, _ := strings.Cut(name, ",")
			assert.PanicsWithValue(t, "specout: GET /x: invalid JSON field name "+strconv.Quote(field), func() {
				docOf(t, http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: value.Interface()}))
			}, "invalid JSON names must not produce a misleading schema")
		})
	}
}

func TestQuotedNumberEnum(t *testing.T) {
	type response struct {
		Value int `json:"value,string" jsonschema:"enum=1,enum=2"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"quoted numeric enum", schema, response{Value: 1}, true},
		payloadCase{"outside quoted numeric enum", schema, response{Value: 3}, false},
	)
}

func TestNamedJSONEmbeddingFailsClearly(t *testing.T) {
	type inner struct {
		Name *string `json:"name"`
	}
	for _, option := range []string{"inline", "embed"} {
		t.Run(option, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{Name: "Inner", Type: reflect.TypeFor[inner](), Tag: reflect.StructTag("json:" + strconv.Quote(","+option))}})
			assert.PanicsWithValue(t, "specout: GET /x: "+typ.String()+" field Inner: the inline or embed option of a json tag needs an anonymous struct field", func() {
				docOf(t, http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: reflect.New(typ).Elem().Interface()}))
			})
		})
	}
}

func TestMixedEnumSyntax(t *testing.T) {
	type request struct {
		State string `jsonschema:"enum=ready|done,enum=done|failed" query:"state"`
	}
	type response struct {
		State string `json:"state" jsonschema:"enum=ready|done,enum=done|failed"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, response]{HandlerFunc: noop})
	assert.Equal(t, []any{"ready", "done", "failed"}, props(t, doc, "response")["state"].(map[string]any)["enum"])
	schema := responseSchemaOf(t, doc, "/x", "get")
	query := paramsOf(t, opOf(t, doc, "/x", "get"))["state"]["schema"]
	exportPayloadCases(t, doc,
		payloadCase{"first mixed enum", schema, response{State: "ready"}, true},
		payloadCase{"last mixed enum", schema, response{State: "failed"}, true},
		payloadCase{"mixed enum parameter", query, "done", true},
		payloadCase{"literal unsplit enum", schema, response{State: "ready|done"}, false},
	)
}

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
	p := props(t, doc, "ReqBody")
	require.Len(t, p, 3, "body props = %v, want id, flags, name", p)
	assert.Equal(t, true, p["flags"].(map[string]any)["readOnly"], "embedded readOnly lost: %v", p["flags"])
	assert.True(t, isNullable(p["id"].(map[string]any)), "embedded pointer not nullable: %v", p["id"])
	assert.NotContains(t, p, "page", "query param leaked into the body")

	params := paramsOf(t, opOf(t, doc, "/x", "post"))
	assert.Len(t, params, 1, "parameters = %v, want [page]", params)
	assert.Contains(t, params, "page")
}

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

func TestEmbeddedShadowingField(t *testing.T) {
	type base struct {
		ID *string `json:"id"`
	}
	type Req struct {
		base

		ID *int64 `json:"id"`
		Q  string `query:"q"`
	}

	p := props(t, docOf(t, "POST", "/x", specout.Handler[Req, specout.NoContent]{HandlerFunc: noop}), "ReqBody")
	require.Len(t, p, 1, "body props = %v, want only the shadowing id", p)
	typ, _ := p["id"].(map[string]any)["type"].([]any)
	assert.Len(t, typ, 2)
	assert.Equal(t, "integer", typ[0], "want the direct *int64 field to win")
}

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

func TestNestedDefsKeepFixups(t *testing.T) {
	type Label struct {
		Color string `json:"color" jsonschema:"pattern=^[0-9a-f]{6}$"`
	}
	type Issue struct {
		ID    int64      `json:"id"            jsonschema:"readonly"`
		State string     `json:"state"         jsonschema:"enum=open|closed,default=open"`
		Label Label      `json:"label"`
		Due   *time.Time `json:"due,omitempty"`
	}
	type Page struct {
		Items []Issue `json:"items"`
	}

	doc := stdDoc(t, func(r *specout.Binder) {
		r.Handle("GET", "/issues", specout.Handler[struct{}, Page]{HandlerFunc: noop})
		r.Handle("POST", "/issues", specout.Handler[struct {
			Title string `json:"title"`
		}, Issue]{HandlerFunc: noop})
	})
	p := props(t, doc, "Issue")
	require.Equal(t, []any{"open", "closed"}, p["state"].(map[string]any)["enum"], "state enum")
	assert.Equal(t, true, p["id"].(map[string]any)["readOnly"], "id readOnly lost")
	assert.True(t, isNullable(p["due"].(map[string]any)), "due wants [string null]")
}

func TestRecursiveNestedDefsFixed(t *testing.T) {
	type Node struct {
		Name string `json:"name"`
		Next *Node  `json:"next,omitempty"`
	}
	type Tree struct {
		Root Node `json:"root"`
	}
	doc := stdDoc(t, func(r *specout.Binder) {
		r.Handle("GET", "/tree", specout.Handler[struct{}, Tree]{HandlerFunc: noop})
		r.Handle("GET", "/pair", specout.Handler[struct{}, recPair]{HandlerFunc: noop})
	})
	for _, c := range []struct{ name, prop string }{
		{"Node", "next"}, {"recA", "b"}, {"recA", "id"}, {"recB", "a"},
	} {
		assert.True(t, isNullable(props(t, doc, c.name)[c.prop].(map[string]any)), "%s.%s has no null arm", c.name, c.prop)
	}
}

func TestNestedOnlyDefsKeepFixups(t *testing.T) {
	type Inner struct {
		ID   int64   `json:"id"            jsonschema:"readonly"`
		Note string  `json:"note"          jsonschema:"deprecated"`
		Due  *string `json:"due,omitempty"`
	}
	type Outer struct {
		Items []Inner `json:"items"`
	}

	p := props(t, docOf(t, "GET", "/outer", specout.Handler[struct{}, Outer]{HandlerFunc: noop}), "Inner")
	assert.Equal(t, true, p["id"].(map[string]any)["readOnly"], "nested readOnly lost")
	assert.Equal(t, true, p["note"].(map[string]any)["deprecated"], "nested deprecated lost")
	assert.True(t, isNullable(p["due"].(map[string]any)), "nested nullable lost")
}

func TestAnonymousComponentNames(t *testing.T) {
	doc := stdDoc(t, func(r *specout.Binder) {
		r.Handle("POST", "/a", specout.Handler[struct {
			A string `json:"a"`
		}, specout.NoContent]{HandlerFunc: noop})
		r.Handle("POST", "/b", specout.Handler[struct {
			B string `json:"b"`
		}, specout.NoContent]{HandlerFunc: noop})
	})
	for name := range schemas(t, doc) {
		assert.NotRegexp(t, "^struct", name, "raw struct literal leaked as component name")
	}
}

type enumStatus string

func (enumStatus) JSONSchema() *specout.Schema { return specout.Enum[enumStatus]("todo", "done") }

type enumTask struct {
	Status enumStatus `json:"status"`
}

type enumQuery struct {
	Status enumStatus `query:"status,omitempty"`
}

func TestEnumIsSharedByEachUse(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/tasks", specout.Handler[enumQuery, []enumTask]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	want := map[string]any{"type": "string", "enum": []any{"todo", "done"}}
	assert.Equal(t, want, schemas(t, doc)["enumStatus"])
	param := paramsOf(t, opOf(t, doc, "/tasks", "get"))["status"]
	assert.Equal(t, want, param["schema"])
}

func TestEnumNeedsUniqueValues(t *testing.T) {
	assert.Contains(t, panicMessage(t, func() { specout.Enum[string]() }), "Enum needs at least one value")
	assert.Contains(t, panicMessage(t, func() { specout.Enum("a", "b", "a") }), `value "a" is repeated`)
}

type uploadReq struct {
	File specout.File `json:"file" jsonschema:"description=CSV"`
}

type (
	emptyMarker struct{}
	item        struct {
		ID string `json:"id"`
	}
)

func TestMarkerTypes(t *testing.T) {
	d := newGen()
	r := http.NewServeMux()
	b := specout.Bind(d, specoutstd.New(r))
	b.Handle("POST", "/files", specout.Handler[uploadReq, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	})
	b.Handle("POST", "/items", specout.Handler[emptyMarker, item]{
		HandlerFunc: func(http.ResponseWriter, *http.Request) {},
		Responses: []specout.Response{
			{Status: 201, Headers: []specout.Header{{Name: "Location"}}},
		},
	})
	doc := buildDoc(t, d)
	paths := pathsObj(doc)

	files := paths["/files"].(map[string]any)["post"].(map[string]any)
	assert.Contains(t, files["responses"].(map[string]any), "204", "NoContent did not produce 204")
	content := files["requestBody"].(map[string]any)["content"].(map[string]any)
	require.Contains(t, content, "multipart/form-data", "File did not produce multipart")
	fileProps := doc["components"].(map[string]any)["schemas"].(map[string]any)["uploadReq"].(map[string]any)["properties"].(map[string]any)
	f := fileProps["file"].(map[string]any)
	assert.Equal(t, "binary", f["format"], "File property format")
	assert.Equal(t, "string", f["type"], "File property type")

	items := paths["/items"].(map[string]any)["post"].(map[string]any)
	hdrs := items["responses"].(map[string]any)["201"].(map[string]any)["headers"].(map[string]any)
	assert.Contains(t, hdrs, "Location")
}

type Node struct {
	Val      string `json:"val"`
	Children []Node `json:"children"`
	Next     *Node  `json:"next,omitempty"`
}

type MapWrap struct {
	Counts map[string]int             `json:"counts"`
	Meta   map[string]struct{ X int } `json:"meta"`
}

type Page[T any] struct{ Items []T }

type (
	Alpha struct{ V string }
	Beta  struct{ W string }
)

type SliceQueryReq struct {
	Tags []string `query:"tags"`
	IDs  []int    `query:"ids"`
}

type StructQueryReq struct {
	Filter struct{ Min int } `query:"filter"`
	Name   string            `json:"name"`
}

type BothTagReq struct {
	Limit int `json:"limit" query:"limit"`
}

type UnregisteredUnionReq struct {
	Kind string `json:"kind" jsonschema:"enum=a|b,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=a|b"`
}

type WildcardReq struct {
	Rest string `query:"rest"`
}

func TestEdgeCases(t *testing.T) {
	type testCase struct {
		name  string
		route func(*specout.Generator)
		check func(*testing.T, map[string]any)
	}
	cases := []testCase{
		{name: "recursive", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/tree", specout.Handler[struct{}, Node]{HandlerFunc: noop})
		}},
		{name: "maps", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/m", specout.Handler[struct{}, MapWrap]{HandlerFunc: noop})
		}},
		{
			name: "generic collision",
			route: func(d *specout.Generator) {
				d.Document(http.MethodGet, "/a", specout.Handler[struct{}, Page[Alpha]]{HandlerFunc: noop})
				d.Document(http.MethodPost, "/b", specout.Handler[struct{}, Page[Beta]]{HandlerFunc: noop})
			},
			check: func(t *testing.T, doc map[string]any) {
				t.Helper()
				for _, want := range []string{"PageAlpha", "PageBeta"} {
					assert.Contains(t, schemas(t, doc), want, "missing component")
				}
			},
		},
		{name: "slice query params", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/q", specout.Handler[SliceQueryReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "struct query param", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/q", specout.Handler[StructQueryReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "query+json both", route: func(d *specout.Generator) {
			d.Document(http.MethodPost, "/q", specout.Handler[BothTagReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "public without auth", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/p", specout.Handler[struct{}, Alpha]{HandlerFunc: noop, Public: true})
		}},
		{name: "unusual statuses", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/s", specout.Handler[struct{}, Alpha]{
				HandlerFunc: noop,
				Responses:   []specout.Response{{Status: 304}, {Status: 599}, {Key: "default"}},
			})
		}},
		{name: "path wildcard param", route: func(d *specout.Generator) {
			d.Document(http.MethodGet, "/files/{path...}", specout.Handler[WildcardReq, struct{}]{HandlerFunc: noop})
		}},
		{
			name: "duplicate param name",
			route: func(d *specout.Generator) {
				type DupReq struct {
					ID string `query:"id"`
				}
				d.Document(http.MethodGet, "/things/{id}", specout.Handler[DupReq, struct{}]{HandlerFunc: noop})
			},
			check: func(t *testing.T, doc map[string]any) {
				t.Helper()
				raw, _ := opOf(t, doc, "/things/{id}", "get")["parameters"].([]any)
				assert.Len(t, raw, 2, "parameters")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newGen()
			c.route(d)
			doc := buildDoc(t, d)
			if c.check != nil {
				c.check(t, doc)
			}
		})
	}
}

func TestUnregisteredUnionFailsClearly(t *testing.T) {
	d := newGen()
	d.Document(http.MethodPost, "/u", specout.Handler[UnregisteredUnionReq, struct{}]{HandlerFunc: noop})
	assert.PanicsWithValue(t, "specout: union variant \"a\" is not registered; call doc.Variant[T](\"a\")", func() { buildDoc(t, d) })
}

type customRequired struct {
	Name string `json:"name,omitempty"`
}

func (customRequired) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Required = []string{"name"}
}

func TestSchemaFixesPreserveCustomRequiredFields(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, customRequired]{HandlerFunc: noop})
	assert.Contains(t, componentsSchema(t, doc, "customRequired")["required"], "name")
}

func TestClosedNullableAnonymousObjects(t *testing.T) {
	type response struct {
		Detail *struct {
			Name string `json:"name"`
		} `json:"detail"`
		Empty *struct{} `json:"empty"`
	}
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
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
			doc := docOf(t, http.MethodGet, "/x", okWith(specout.Response{Status: 200, Type: value}))
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
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, labels]{HandlerFunc: noop})
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
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, recursiveParameter]{HandlerFunc: noop})
		doc := buildDoc(t, d)
		assert.Contains(t, schemas(t, doc), "Current")
		assert.NotContains(t, schemas(t, doc), "Previous")
	}
}

func TestSchemaTagSpacesAndMissingValuesPanic(t *testing.T) {
	type spaced struct {
		Age int `json:"age" jsonschema:"required, minimum=1"`
	}
	type bare struct {
		Name string `json:"name" jsonschema:"minLength"`
	}
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodPost, "/x", specout.Handler[spaced, job]{HandlerFunc: noop})
	}), `jsonschema keyword " minimum" starts or ends with a space; remove the space`)
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodPost, "/x", specout.Handler[bare, job]{HandlerFunc: noop})
	}), `jsonschema keyword "minLength" has no value; write minLength=<value>`)
}

func TestUnknownSchemaTypesPanic(t *testing.T) {
	type noDiscriminator struct {
		Data any `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type semicolons struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email;slack"`
	}
	type typeTag struct {
		Name string `json:"name" jsonschema:"type=text"`
	}
	for want, value := range map[string]any{
		`"email|slack"`: noDiscriminator{},
		`"email"`:       semicolons{},
		`"text"`:        typeTag{},
	} {
		t.Run(want, func(t *testing.T) {
			d := newGen()
			d.Variant[unionEmail]("email")
			d.Variant[unionSlack]("slack")
			d.Document(http.MethodGet, "/x", okWith(specout.Response{Status: http.StatusOK, Type: value}))
			assert.PanicsWithValue(t, "specout: schema type "+want+" is not a JSON Schema type; "+
				"a oneof_type union separates its variants with | and needs a Discriminator field", func() { _, _ = d.Build() })
		})
	}
}
