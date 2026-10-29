package specout_test

import (
	jsonv1 "encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodPost, "/union", specout.Handler[unionEnvelope, specout.NoContent]{HandlerFunc: noop})
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

// closedUnionSchemas documents Req on POST path, with ClosedSchemas set and
// both union branches registered, and returns the component schemas.
func closedUnionSchemas[Req any](t *testing.T, path string) map[string]any {
	t.Helper()
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodPost, path, specout.Handler[Req, specout.NoContent]{HandlerFunc: noop})
	return schemas(t, buildDoc(t, d))
}

// assertClosedUnion checks that the unionEnvelope root stays open, so that
// oneOf can choose a branch, and that each branch is closed.
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
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodPost, "/nested-union", specout.Handler[unionNestedHolder, specout.NoContent]{HandlerFunc: noop})
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
		d.Register[unionEmail]("email")
		d.Register[unionSlack]("slack")
		root := specout.Get[unionEnvelope]{HandlerFunc: noop}
		holder := specout.Get[unionNamedHolder]{HandlerFunc: noop}
		if rootFirst {
			specout.Document(d, http.MethodGet, "/union", root)
		}
		specout.Document(d, http.MethodGet, "/holder", holder)
		if !rootFirst {
			specout.Document(d, http.MethodGet, "/union", root)
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

func TestLaunchSingleVariantUnion(t *testing.T) {
	type envelope struct {
		Kind string `json:"kind" jsonschema:"enum=email,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email"`
	}
	d := newGen()
	d.Register[unionEmail]("email")
	specout.Document(d, http.MethodGet, "/x", specout.Get[envelope]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"single variant", schema, envelope{Kind: "email", Data: unionEmail{Address: "ops@example.com"}}, true},
		launchSample{"wrong single variant payload", schema, map[string]any{"kind": "email", "data": map[string]any{"channel": "#ops"}}, false},
	)
	assert.Len(t, schemas(t, doc)["envelope"].(map[string]any)["oneOf"], 1)
}

func TestLaunchUnionPayloadKeepsItsOptionality(t *testing.T) {
	type optional struct {
		Kind string `json:"kind"           jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `json:"data,omitempty" jsonschema:"oneof_type=email|slack"`
	}
	type nullable struct {
		Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data *any   `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	d := newGen()
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodGet, "/optional", specout.Get[optional]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/nullable", specout.Get[nullable]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	var email any = unionEmail{Address: "ops@example.com"}
	optionalSchema := launchResponseSchema(t, doc, "/optional", "get")
	nullableSchema := launchResponseSchema(t, doc, "/nullable", "get")
	exportLaunchSamples(t, doc,
		launchSample{"optional payload", optionalSchema, optional{Kind: "email", Data: email}, true},
		launchSample{"omitted payload", optionalSchema, optional{Kind: "email"}, true},
		launchSample{"wrong optional payload", optionalSchema, optional{Kind: "slack", Data: email}, false},
		launchSample{"nullable payload", nullableSchema, nullable{Kind: "email", Data: &email}, true},
		launchSample{"null payload", nullableSchema, nullable{Kind: "email"}, true},
		launchSample{"wrong nullable payload", nullableSchema, nullable{Kind: "slack", Data: &email}, false},
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
			d.Register[unionEmail]("email")
			d.Register[unionSlack]("slack")
			specout.Document(d, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: value}))
			assert.PanicsWithValue(t, "specout: schema type "+want+" is not a JSON Schema type; "+
				"a oneof_type union separates its variants with | and needs a Discriminator field", func() { _ = d.WriteJSON(io.Discard) })
		})
	}
}

// A payload field that holds its variants in a list or a map gets a branch
// payload of the same shape, one variant per element.
func TestLaunchUnionPayloadInContainers(t *testing.T) {
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
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodGet, "/listed", specout.Get[listed]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/mapped", specout.Get[mapped]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/pointers", specout.Get[pointers]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/fixed", specout.Get[fixed]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	var email any = unionEmail{Address: "ops@example.com"}
	listedSchema := launchResponseSchema(t, doc, "/listed", "get")
	mappedSchema := launchResponseSchema(t, doc, "/mapped", "get")
	pointersSchema := launchResponseSchema(t, doc, "/pointers", "get")
	exportLaunchSamples(t, doc,
		launchSample{"listed payload", listedSchema, listed{Kind: "email", Data: []any{email, email}}, true},
		launchSample{"unlisted payload", listedSchema, map[string]any{"kind": "email", "data": email}, false},
		launchSample{"wrong listed payload", listedSchema, listed{Kind: "slack", Data: []any{email}}, false},
		launchSample{"mapped payload", mappedSchema, mapped{Kind: "email", Data: map[string]any{"a": email}}, true},
		launchSample{"wrong mapped payload", mappedSchema, mapped{Kind: "slack", Data: map[string]any{"a": email}}, false},
		launchSample{"null element", pointersSchema, pointers{Kind: "email", Data: []*any{&email, nil}}, true},
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

func TestLaunchRawUnionPayloadIsTheVariant(t *testing.T) {
	type raw struct {
		Kind string            `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data jsonv1.RawMessage `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type nested struct {
		Kind string  `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data [][]any `json:"data" jsonschema:"oneof_type=email|slack"`
	}
	d := newGen()
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodGet, "/raw", specout.Get[raw]{HandlerFunc: noop})
	specout.Document(d, http.MethodGet, "/nested", specout.Get[nested]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	schema := launchResponseSchema(t, doc, "/raw", "get")
	exportLaunchSamples(t, doc,
		launchSample{"raw payload", schema, raw{Kind: "email", Data: jsonv1.RawMessage(`{"address":"ops@example.com"}`)}, true},
		launchSample{"listed raw payload", schema, raw{Kind: "email", Data: jsonv1.RawMessage(`[{"address":"ops@example.com"}]`)}, false},
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
