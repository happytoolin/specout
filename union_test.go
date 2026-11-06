package specout_test

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnionConstrainsTheCompleteEnvelope(t *testing.T) {
	d := newGen()
	d.Variant[schemaEmail]("email")
	d.Variant[schemaSlack]("slack")
	d.Document(http.MethodPost, "/union", specout.Handler[schemaUnion, specout.NoContent]{HandlerFunc: noop})
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

type listItem struct {
	Name string `json:"name"`
}

type nestedEnvelope struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}

type nestedEnvelopeVariant1 struct {
	Other int `json:"other"`
}

type recursiveEmbedding struct{ *recursiveEmbedding }

type pointerCycle *pointerCycle

type payloadCase struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Value  any    `json:"value"`
	Valid  bool   `json:"valid"`
}

func exportPayloadCases(t *testing.T, doc map[string]any, samples ...payloadCase) {
	t.Helper()
	if os.Getenv("SPECOUT_VALIDATE_DIR") == "" {
		return
	}
	doc["x-specout-test-cases"] = samples
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	exportValidationDoc(t, data)
}

func responseSchemaOf(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	response := opOf(t, doc, path, method)["responses"].(map[string]any)["200"].(map[string]any)
	return response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
}

func TestUnionNameCollision(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			d := newGen()
			d.Variant[schemaEmail]("email")
			d.Variant[schemaSlack]("slack")
			union := func() {
				d.Document(http.MethodGet, "/union", specout.Handler[struct{}, nestedEnvelope]{HandlerFunc: noop})
			}
			other := func() {
				d.Document(http.MethodGet, "/other", specout.Handler[struct{}, nestedEnvelopeVariant1]{HandlerFunc: noop})
			}
			if reverse {
				other()
				union()
			} else {
				union()
				other()
			}
			doc := buildDoc(t, d)
			schema := responseSchemaOf(t, doc, "/union", "get")
			exportPayloadCases(t, doc,
				payloadCase{"valid union", schema, nestedEnvelope{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true},
				payloadCase{"unrelated object", schema, nestedEnvelopeVariant1{Other: 1}, false},
			)
			branch := componentsSchema(t, doc, "nestedEnvelope")["oneOf"].([]any)[0].(map[string]any)["$ref"]
			assert.NotEqual(t, responseSchemaOf(t, doc, "/other", "get")["$ref"], branch,
				"a union branch must not reference an unrelated response type")
		})
	}
}

func TestFormUnionAndParameterUnion(t *testing.T) {
	type form struct {
		Kind string `form:"event"   json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `form:"payload" json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type request struct {
		Filter nestedEnvelope `jsonschema:"style=deepObject" query:"filter"`
	}
	d := newGen()
	d.Variant[schemaEmail]("email")
	d.Variant[schemaSlack]("slack")
	d.Document(http.MethodPost, "/form", specout.Handler[form, form]{HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{"application/x-www-form-urlencoded"}}})
	d.Document(http.MethodGet, "/query", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	body := opOf(t, doc, "/form", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/x-www-form-urlencoded"].(map[string]any)["schema"]
	parameter := paramsOf(t, opOf(t, doc, "/query", "get"))["filter"]["schema"]
	exportPayloadCases(t, doc,
		payloadCase{"form union", body, map[string]any{"event": "email", "payload": map[string]any{"address": "ops@example.com"}}, true},
		payloadCase{"form union mismatch", body, map[string]any{"event": "email", "payload": map[string]any{"channel": "alerts"}}, false},
		payloadCase{"parameter union", parameter, nestedEnvelope{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true},
		payloadCase{"parameter union mismatch", parameter, nestedEnvelope{Kind: "email", Data: schemaSlack{Channel: "alerts"}}, false},
	)
}

func TestUnionNameCollisionWithNestedComponent(t *testing.T) {
	type response struct {
		Value nestedEnvelopeVariant1 `json:"value"`
	}
	d := newGen()
	d.Variant[schemaEmail]("email")
	d.Variant[schemaSlack]("slack")
	d.Document(http.MethodGet, "/union", specout.Handler[struct{}, nestedEnvelope]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/nested", specout.Handler[struct{}, response]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	schema := responseSchemaOf(t, doc, "/union", "get")
	exportPayloadCases(t, doc,
		payloadCase{"union with later nested collision", schema, nestedEnvelope{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true},
		payloadCase{"unrelated nested component", schema, nestedEnvelopeVariant1{Other: 1}, false},
	)
}

func TestDuplicateUnionNames(t *testing.T) {
	type response struct {
		Kind string `json:"kind" jsonschema:"enum=email|email,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email|email"`
	}
	d := newGen()
	d.Variant[schemaEmail]("email")
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, response]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	exportPayloadCases(t, doc, payloadCase{"repeated union variant", responseSchemaOf(t, doc, "/x", "get"), response{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true})
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
		d.Variant[unionEmail]("email")
		d.Variant[unionSlack]("slack")
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, nullableUnionHolder]{HandlerFunc: noop})
		event := props(t, buildDoc(t, d), "nullableUnionHolder")["event"].(map[string]any)
		assert.Contains(t, event["oneOf"], map[string]any{"type": "null"})
		assert.NotContains(t, event, "type")
	}
}

type unionEmail struct {
	Address string `json:"address"`
}

type unionSlack struct {
	Channel string `json:"channel"`
}

type unionEnvelope struct {
	Kind    string        `json:"kind"               jsonschema:"enum=email|slack,description=Discriminator"`
	Data    any           `json:"data"               jsonschema:"oneof_type=email|slack"`
	TraceID *string       `json:"trace_id,omitempty"`
	Meta    unionMetadata `json:"meta"`
}

type unionMetadata struct {
	Source string  `json:"source"`
	Note   *string `json:"note,omitempty"`
}

type unionNestedHolder struct {
	Email struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
	} `json:"email"`
	Slack struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
	} `json:"slack"`
}

type unionNamedHolder struct {
	Envelope unionEnvelope `json:"envelope"`
}

func TestUnionBranchesPreserveEnvelopeFieldsAndFixups(t *testing.T) {
	d := newGen()
	d.Variant[unionEmail]("email")
	d.Variant[unionSlack]("slack")
	d.Document(http.MethodPost, "/union", specout.Handler[unionEnvelope, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	components := schemas(t, doc)
	root := components["unionEnvelope"].(map[string]any)
	for _, raw := range root["oneOf"].([]any) {
		ref := raw.(map[string]any)["$ref"].(string)
		branch := components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		props := branch["properties"].(map[string]any)
		assert.Contains(t, props, "trace_id")
		assert.Contains(t, props, "meta")
		assert.Contains(t, branch["required"], "meta")
		assert.True(t, isNullable(props["trace_id"].(map[string]any)))
		metaRef := props["meta"].(map[string]any)["$ref"].(string)
		meta := components[strings.TrimPrefix(metaRef, "#/components/schemas/")].(map[string]any)
		assert.True(t, isNullable(meta["properties"].(map[string]any)["note"].(map[string]any)))
	}
}

func closedUnionSchemas[Req any](t *testing.T, path string) map[string]any {
	t.Helper()
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Variant[unionEmail]("email")
	d.Variant[unionSlack]("slack")
	d.Document(http.MethodPost, path, specout.Handler[Req, specout.NoContent]{HandlerFunc: noop})
	return schemas(t, buildDoc(t, d))
}

func assertClosedUnion(t *testing.T, components map[string]any) {
	t.Helper()
	union := components["unionEnvelope"].(map[string]any)
	assert.NotContains(t, union, "additionalProperties")
	for _, raw := range union["oneOf"].([]any) {
		ref := raw.(map[string]any)["$ref"].(string)
		branch := components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		assert.Equal(t, false, branch["additionalProperties"])
	}
}

func TestUnionBranchesCloseObjects(t *testing.T) {
	assertClosedUnion(t, closedUnionSchemas[unionEnvelope](t, "/union"))
}

func TestClosedNestedUnionRootValidatesPayload(t *testing.T) {
	root := closedUnionSchemas[unionNestedHolder](t, "/nested-union")["unionNestedHolder"].(map[string]any)
	for _, name := range []string{"email", "slack"} {
		child := root["properties"].(map[string]any)[name].(map[string]any)
		assert.NotContains(t, child, "additionalProperties")
	}
}

func TestClosedNamedUnionRootValidatesPayload(t *testing.T) {
	assertClosedUnion(t, closedUnionSchemas[unionNamedHolder](t, "/named-union"))
}

func TestNestedUnionsUseDistinctBranchNames(t *testing.T) {
	d := newGen()
	d.Variant[unionEmail]("email")
	d.Variant[unionSlack]("slack")
	d.Document(http.MethodPost, "/nested-union", specout.Handler[unionNestedHolder, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	root := schemas(t, doc)["unionNestedHolder"].(map[string]any)
	properties := root["properties"].(map[string]any)
	refs := map[string]bool{}
	for _, name := range []string{"email", "slack"} {
		child := properties[name].(map[string]any)
		for _, raw := range child["oneOf"].([]any) {
			ref := raw.(map[string]any)["$ref"].(string)
			require.False(t, refs[ref], "nested union branch collision: %s", ref)
			refs[ref] = true
		}
	}
	assert.Len(t, refs, 4)
}

func TestUnionUsedAsRootAndFieldHasOneSetOfBranches(t *testing.T) {
	for _, rootFirst := range []bool{true, false} {
		d := newGen()
		d.Variant[unionEmail]("email")
		d.Variant[unionSlack]("slack")
		root := specout.Handler[struct{}, unionEnvelope]{HandlerFunc: noop}
		holder := specout.Handler[struct{}, unionNamedHolder]{HandlerFunc: noop}
		if rootFirst {
			d.Document(http.MethodGet, "/union", root)
		}
		d.Document(http.MethodGet, "/holder", holder)
		if !rootFirst {
			d.Document(http.MethodGet, "/union", root)
		}
		var branches []string
		for name := range schemas(t, buildDoc(t, d)) {
			if strings.HasPrefix(name, "unionEnvelopeVariant") {
				branches = append(branches, name)
			}
		}
		assert.ElementsMatch(t, []string{"unionEnvelopeVariant1", "unionEnvelopeVariant2"}, branches, "rootFirst=%t", rootFirst)
	}
}

func TestSingleVariantUnion(t *testing.T) {
	type envelope struct {
		Kind string `json:"kind" jsonschema:"enum=email,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email"`
	}
	d := newGen()
	d.Variant[unionEmail]("email")
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, envelope]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	schema := responseSchemaOf(t, doc, "/x", "get")
	exportPayloadCases(t, doc,
		payloadCase{"single variant", schema, envelope{Kind: "email", Data: unionEmail{Address: "ops@example.com"}}, true},
		payloadCase{"wrong single variant payload", schema, map[string]any{"kind": "email", "data": map[string]any{"channel": "#ops"}}, false},
	)
	assert.Len(t, schemas(t, doc)["envelope"].(map[string]any)["oneOf"], 1)
}

func TestUnionPayloadKeepsItsOptionality(t *testing.T) {
	type optional struct {
		Kind string `json:"kind"           jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `json:"data,omitempty" jsonschema:"oneof_type=email|slack"`
	}
	type nullable struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data *any   `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	d := newGen()
	d.Variant[unionEmail]("email")
	d.Variant[unionSlack]("slack")
	d.Document(http.MethodGet, "/optional", specout.Handler[struct{}, optional]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/nullable", specout.Handler[struct{}, nullable]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	var email any = unionEmail{Address: "ops@example.com"}
	optionalSchema := responseSchemaOf(t, doc, "/optional", "get")
	nullableSchema := responseSchemaOf(t, doc, "/nullable", "get")
	exportPayloadCases(t, doc,
		payloadCase{"optional payload", optionalSchema, optional{Kind: "email", Data: email}, true},
		payloadCase{"omitted payload", optionalSchema, optional{Kind: "email"}, true},
		payloadCase{"wrong optional payload", optionalSchema, optional{Kind: "slack", Data: email}, false},
		payloadCase{"nullable payload", nullableSchema, nullable{Kind: "email", Data: &email}, true},
		payloadCase{"null payload", nullableSchema, nullable{Kind: "email"}, true},
		payloadCase{"wrong nullable payload", nullableSchema, nullable{Kind: "slack", Data: &email}, false},
	)
	components := schemas(t, doc)
	for name, wantRequired := range map[string][]any{
		"optional": {"kind"},
		"nullable": {"kind", "data"},
	} {
		for _, raw := range components[name].(map[string]any)["oneOf"].([]any) {
			ref := raw.(map[string]any)["$ref"].(string)
			branch := components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
			assert.ElementsMatch(t, wantRequired, branch["required"], name)
		}
	}
	nullBranch := components["nullableVariant1"].(map[string]any)["properties"].(map[string]any)["data"]
	assert.Equal(t, []any{map[string]any{"$ref": "#/components/schemas/unionEmail"}, map[string]any{"type": "null"}},
		nullBranch.(map[string]any)["anyOf"])
}

func TestUnionPayloadInContainers(t *testing.T) {
	type listed struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data []any  `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type mapped struct {
		Kind string         `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data map[string]any `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type pointers struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data []*any `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type fixed struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data [2]any `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	d := newGen()
	d.Variant[unionEmail]("email")
	d.Variant[unionSlack]("slack")
	d.Document(http.MethodGet, "/listed", specout.Handler[struct{}, listed]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/mapped", specout.Handler[struct{}, mapped]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/pointers", specout.Handler[struct{}, pointers]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/fixed", specout.Handler[struct{}, fixed]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	var email any = unionEmail{Address: "ops@example.com"}
	listedSchema := responseSchemaOf(t, doc, "/listed", "get")
	mappedSchema := responseSchemaOf(t, doc, "/mapped", "get")
	pointersSchema := responseSchemaOf(t, doc, "/pointers", "get")
	exportPayloadCases(t, doc,
		payloadCase{"listed payload", listedSchema, listed{Kind: "email", Data: []any{email, email}}, true},
		payloadCase{"unlisted payload", listedSchema, map[string]any{"kind": "email", "data": email}, false},
		payloadCase{"wrong listed payload", listedSchema, listed{Kind: "slack", Data: []any{email}}, false},
		payloadCase{"mapped payload", mappedSchema, mapped{Kind: "email", Data: map[string]any{"a": email}}, true},
		payloadCase{"wrong mapped payload", mappedSchema, mapped{Kind: "slack", Data: map[string]any{"a": email}}, false},
		payloadCase{"null element", pointersSchema, pointers{Kind: "email", Data: []*any{&email, nil}}, true},
	)
	components := schemas(t, doc)
	data := func(branch string) any {
		return components[branch].(map[string]any)["properties"].(map[string]any)["data"]
	}
	emailRef := map[string]any{"$ref": "#/components/schemas/unionEmail"}
	assert.Equal(t, map[string]any{"type": "array", "items": emailRef}, data("listedVariant1"))
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": emailRef}, data("mappedVariant1"))
	assert.Equal(t, map[string]any{"type": "array", "items": map[string]any{
		"anyOf": []any{emailRef, map[string]any{"type": "null"}},
	}}, data("pointersVariant1"))
	assert.Equal(t, map[string]any{"type": "array", "items": emailRef, "minItems": float64(2), "maxItems": float64(2)},
		data("fixedVariant1"))
}

func TestRawUnionPayloadIsTheVariant(t *testing.T) {
	type raw struct {
		Kind string            `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data jsonv1.RawMessage `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type nested struct {
		Kind string  `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data [][]any `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	d := newGen()
	d.Variant[unionEmail]("email")
	d.Variant[unionSlack]("slack")
	d.Document(http.MethodGet, "/raw", specout.Handler[struct{}, raw]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/nested", specout.Handler[struct{}, nested]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	schema := responseSchemaOf(t, doc, "/raw", "get")
	exportPayloadCases(t, doc,
		payloadCase{"raw payload", schema, raw{Kind: "email", Data: jsonv1.RawMessage(`{"address":"ops@example.com"}`)}, true},
		payloadCase{"listed raw payload", schema, raw{Kind: "email", Data: jsonv1.RawMessage(`[{"address":"ops@example.com"}]`)}, false},
	)
	components := schemas(t, doc)
	data := func(branch string) any {
		return components[branch].(map[string]any)["properties"].(map[string]any)["data"]
	}
	emailRef := map[string]any{"$ref": "#/components/schemas/unionEmail"}
	assert.Equal(t, emailRef, data("rawVariant1"))
	assert.Equal(t, map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": emailRef}},
		data("nestedVariant1"))
}
