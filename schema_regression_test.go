package specout_test

import (
	jsonv1 "encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

// schemaHiddenEmbed is embedded by a test type with a hidden field.
type schemaHiddenEmbed struct {
	Secret string `json:"secret" jsonschema:"-"`
}

// jsonschema:"-" hides a field that JSON still writes, so a closed schema
// would reject every real payload. json:"-" hides it from both.
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
			specout.Document(d, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: body}))
			assert.PanicsWithValue(t, "specout: specout_test."+want+` has jsonschema:"-", but JSON still writes it, `+
				`and ClosedSchemas rejects it; tag it json:"-", or drop ClosedSchemas`, func() { _ = d.WriteJSON(io.Discard) })
		})
	}
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	specout.Document(d, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: ignored{}}))
	assert.NotContains(t, props(t, buildDoc(t, d), "ignored"), "secret")
	open := newGen()
	specout.Document(open, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: direct{}}))
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

func TestLaunchNumberEnumTagsSplitOnPipes(t *testing.T) {
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
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"listed numbers", schema, response{Code: 2, Mixed: 1, Ratio: 1.5, List: []int{7}}, true},
		launchSample{"unlisted number", schema, response{Code: 3, Mixed: 3, Ratio: 0.5}, false},
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

// schemaTextID has no exported fields: the JSON encoders write it only
// through MarshalText.
type schemaTextID struct{ hi, lo uint64 }

func (id schemaTextID) MarshalText() ([]byte, error) {
	if id == (schemaTextID{}) {
		return nil, errors.New("empty ID")
	}
	return []byte(strconv.FormatUint(id.hi, 16) + "-" + strconv.FormatUint(id.lo, 16)), nil
}

// schemaTextKey is an integer that the encoders write through MarshalText,
// also as a map key.
type schemaTextKey int

func (k schemaTextKey) MarshalText() ([]byte, error) {
	if k < 0 {
		return nil, errors.New("negative key")
	}
	return []byte("key-" + strconv.Itoa(int(k))), nil
}

func TestLaunchTextMarshalersAreStrings(t *testing.T) {
	type response struct {
		ID   schemaTextID          `json:"id"`
		Ptr  *schemaTextID         `json:"ptr"`
		Addr netip.Addr            `json:"addr"`
		Keys map[schemaTextKey]int `json:"keys"`
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/id", specout.Get[schemaTextID]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	value := response{ID: schemaTextID{1, 2}, Addr: netip.MustParseAddr("::1"), Keys: map[schemaTextKey]int{5: 1}}
	exportLaunchSamples(t, doc,
		launchSample{"text values", launchResponseSchema(t, doc, "/x", "get"), value, true},
		launchSample{"text root", launchResponseSchema(t, doc, "/id", "get"), schemaTextID{3, 4}, true},
		launchSample{"object for a text value", launchResponseSchema(t, doc, "/id", "get"), map[string]any{}, false},
	)
	p := props(t, doc, "response")
	assert.Equal(t, "string", p["id"].(map[string]any)["type"])
	assert.Equal(t, []any{"string", "null"}, p["ptr"].(map[string]any)["type"])
	assert.Equal(t, "string", p["addr"].(map[string]any)["type"])
	assert.NotContains(t, p["keys"], "patternProperties", "MarshalText writes the keys, not the integers")
	assert.Equal(t, "string", componentsSchema(t, doc, "schemaTextID")["type"])
}

func TestLaunchUintptrIsAnInteger(t *testing.T) {
	type response struct {
		Address uintptr `json:"address"`
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/root", specout.Get[uintptr]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	exportLaunchSamples(t, doc,
		launchSample{"uintptr field", launchResponseSchema(t, doc, "/x", "get"), response{Address: 42}, true},
		launchSample{"uintptr root", launchResponseSchema(t, doc, "/root", "get"), uintptr(7), true},
		launchSample{"string for uintptr", launchResponseSchema(t, doc, "/root", "get"), "7", false},
	)
	assert.Equal(t, "integer", props(t, doc, "response")["address"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "integer"}, launchResponseSchema(t, doc, "/root", "get"))
}

// schemaStamp gets MarshalJSON from its embedded time.Time, so the encoders
// write the whole struct as a time string, without the note.
type schemaStamp struct {
	time.Time

	Note string `json:"note"`
}

// schemaOwnStamp declares its own MarshalJSON, which wins over the promoted
// one.
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

func TestLaunchPromotedEncoderSetsTheSchema(t *testing.T) {
	type response struct {
		At   schemaStamp   `json:"at"`
		Ptr  *schemaStamp  `json:"ptr"`
		List []schemaStamp `json:"list"`
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/stamp", specout.Get[schemaStamp]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/own", specout.Get[schemaOwnStamp]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	stamp := schemaStamp{Time: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC), Note: "dropped"}
	wire, err := jsonv1.Marshal(stamp)
	require.NoError(t, err)
	require.JSONEq(t, `"2026-10-29T12:00:00Z"`, string(wire), "encoding/json writes the embedded time")
	value := response{At: stamp, List: []schemaStamp{stamp}}
	exportLaunchSamples(t, doc,
		launchSample{"promoted encoder", launchResponseSchema(t, doc, "/x", "get"), value, true},
		launchSample{"promoted root", launchResponseSchema(t, doc, "/stamp", "get"), stamp, true},
		launchSample{
			"fields for a promoted encoder", launchResponseSchema(t, doc, "/stamp", "get"),
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

// The math/big types write a number or text through pointer methods, so the
// reflector would read their Go fields.
func TestLaunchBigNumbers(t *testing.T) {
	type response struct {
		Int   *big.Int   `json:"int"`
		Float *big.Float `json:"float"`
		Rat   *big.Rat   `json:"rat"`
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/int", specout.Get[*big.Int]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	huge, ok := new(big.Int).SetString("123456789012345678901234567890", 10)
	require.True(t, ok)
	value := response{Int: huge, Float: big.NewFloat(1.5), Rat: big.NewRat(1, 3)}
	wire, err := jsonv1.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"int":123456789012345678901234567890,"float":"1.5","rat":"1/3"}`, string(wire))
	exportLaunchSamples(t, doc,
		launchSample{"big values", launchResponseSchema(t, doc, "/x", "get"), value, true},
		launchSample{"null big values", launchResponseSchema(t, doc, "/x", "get"), response{}, true},
		launchSample{
			"string for big.Int", launchResponseSchema(t, doc, "/x", "get"),
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
			assert.PanicsWithValue(t, "specout: JSON cannot encode "+want+`; tag its field json:"-"`, func() {
				docOf(t, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: value}))
			})
		})
	}
	type skipped struct {
		F func() `json:"-"`
		A int    `json:"a"`
	}
	assert.NotPanics(t, func() { docOf(t, http.MethodGet, "/x", specout.Get[skipped]{HandlerFunc: noop}) })
}

type (
	dupA struct{ A string }
	dupB struct{ B int }
)

// Contact collides with specout.Contact by name and differs in shape.
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
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/a", specout.Handler[struct{}, dupA]{HandlerFunc: noop})
	specout.Chi(d, r).Get("/b", specout.Handler[struct{}, dupB]{HandlerFunc: noop})
	adopt(t, specout.Chi(d, r))
	require.Panics(t, func() { buildDoc(t, d) }, "expected duplicate-name panic")
}

// Regression: two distinct types with one name, reached only through a nested
// field, used to collapse into a single component — the first shape won and
// every $ref pointed at it, so the second shape was silently documented wrong.
func TestSameNamedNestedTypesPanic(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop})
	adopt(t, specout.Chi(d, r))
	require.Panics(t, func() { buildDoc(t, d) }, "expected a duplicate-name panic")
}

// SchemaName must reach a type that is only ever nested: both components are
// emitted and each $ref points at its own shape.
func TestNestedOnlyOverrideApplies(t *testing.T) {
	d := newGen()
	d.SchemaName[Contact]("LocalContact")
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop})
	doc := serveDoc(t, d, r)

	comps := schemas(t, doc)
	local, ok := comps["LocalContact"].(map[string]any)
	require.True(t, ok, "the override did not apply")
	assert.Contains(t, local["properties"].(map[string]any), "handle", "LocalContact")
	assert.Len(t, comps["Contact"].(map[string]any)["properties"].(map[string]any), 3, "Contact wants the library shape")
	np := props(t, doc, "nestedDup")
	assert.Equal(t, "#/components/schemas/LocalContact", np["local"].(map[string]any)["$ref"], "local ref")
	assert.Equal(t, "#/components/schemas/Contact", np["lib"].(map[string]any)["$ref"], "lib ref")
}

// Anonymous1 is a user type with the name that the first anonymous struct
// would get.
type Anonymous1 struct {
	ID string `json:"id"`
}

// An anonymous struct takes the next AnonymousN name that no named type uses,
// so the user type keeps Anonymous1 whichever route the build reaches first.
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
		specout.Document(d, http.MethodGet, userPath, specout.Get[Anonymous1]{HandlerFunc: noop})
		specout.Document(d, http.MethodGet, anonPath, specout.Get[anonymous]{HandlerFunc: noop})
		doc := buildDoc(t, d)
		assert.Contains(t, props(t, doc, "Anonymous1"), "id", "userFirst=%t", userFirst)
		assert.Contains(t, props(t, doc, "Anonymous2"), "name", "userFirst=%t", userFirst)
		assert.Equal(t, "#/components/schemas/Anonymous2", launchResponseSchema(t, doc, anonPath, "get")["$ref"])
	}
}
