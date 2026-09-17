package specout_test

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
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
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate-name panic")
		}
	}()
	d := newGen()
	d.SchemaName[dupA]("Clash")
	d.SchemaName[dupB]("Clash")
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/a", specout.Handler[struct{}, dupA]{HandlerFunc: noop})
	specout.Chi(d, r).Get("/b", specout.Handler[struct{}, dupB]{HandlerFunc: noop})
	serveDoc(t, d, r)
}

// Regression: two distinct types with one name, reached only through a nested
// field, used to collapse into a single component — the first shape won and
// every $ref pointed at it, so the second shape was silently documented wrong.
func TestSameNamedNestedTypesPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a duplicate-name panic")
		}
	}()
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop})
	serveDoc(t, d, r)
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
	if !ok {
		t.Fatalf("the override did not apply: %v", comps)
	}
	if _, ok := local["properties"].(map[string]any)["handle"]; !ok {
		t.Errorf("LocalContact = %v", local)
	}
	if lib := comps["Contact"].(map[string]any)["properties"].(map[string]any); len(lib) != 3 {
		t.Errorf("Contact = %v, want the library shape", lib)
	}
	np := props(t, doc, "nestedDup")
	if ref := np["local"].(map[string]any)["$ref"]; ref != "#/components/schemas/LocalContact" {
		t.Errorf("local ref = %v", ref)
	}
	if ref := np["lib"].(map[string]any)["$ref"]; ref != "#/components/schemas/Contact" {
		t.Errorf("lib ref = %v", ref)
	}
}
