package specout_test

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dupA struct{ A string }
type dupB struct{ B int }

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
	require.Panics(t, func() { serveDoc(t, d, r) }, "expected duplicate-name panic")
}

// Regression: two distinct types with one name, reached only through a nested
// field, used to collapse into a single component — the first shape won and
// every $ref pointed at it, so the second shape was silently documented wrong.
func TestSameNamedNestedTypesPanic(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop})
	require.Panics(t, func() { serveDoc(t, d, r) }, "expected a duplicate-name panic")
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
