package specout_test

import (
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

func TestUnionBranchesCloseObjects(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodPost, "/union", specout.Handler[unionEnvelope, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	components := schemas(t, doc)
	root := components["unionEnvelope"].(map[string]any)
	assert.NotContains(t, root, "additionalProperties")
	for _, raw := range root["oneOf"].([]any) {
		ref := raw.(map[string]any)["$ref"].(string)
		branch := components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		assert.Equal(t, false, branch["additionalProperties"])
	}
}

func TestClosedNestedUnionRootValidatesPayload(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodPost, "/nested-union", specout.Handler[unionNestedHolder, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	root := schemas(t, doc)["unionNestedHolder"].(map[string]any)
	for _, name := range []string{"email", "slack"} {
		child := root["properties"].(map[string]any)[name].(map[string]any)
		assert.NotContains(t, child, "additionalProperties")
	}
}

func TestClosedNamedUnionRootValidatesPayload(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ClosedSchemas: true})
	d.Register[unionEmail]("email")
	d.Register[unionSlack]("slack")
	specout.Document(d, http.MethodPost, "/named-union", specout.Handler[unionNamedHolder, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	components := schemas(t, doc)
	union := components["unionEnvelope"].(map[string]any)
	assert.NotContains(t, union, "additionalProperties")
	for _, raw := range union["oneOf"].([]any) {
		ref := raw.(map[string]any)["$ref"].(string)
		branch := components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		assert.Equal(t, false, branch["additionalProperties"])
	}
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
