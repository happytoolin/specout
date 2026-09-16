package specout_test

import (
	"net/http"
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	d.SchemaName[dupA]("Clash")
	d.SchemaName[dupB]("Clash")
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/a", specout.Handler[struct{}, dupA]{HandlerFunc: func(http.ResponseWriter, *http.Request) {}})
	specout.Chi(d, r).Get("/b", specout.Handler[struct{}, dupB]{HandlerFunc: func(http.ResponseWriter, *http.Request) {}})
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop2})
	serveDoc(t, d, r)
}

// SchemaName must reach a type that is only ever nested: both components are
// emitted and each $ref points at its own shape.
func TestNestedOnlyOverrideApplies(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	d.SchemaName[Contact]("LocalContact")
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/x", specout.Handler[struct{}, nestedDup]{HandlerFunc: noop2})
	doc := serveDoc(t, d, r)

	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	local, ok := schemas["LocalContact"].(map[string]any)
	if !ok {
		t.Fatalf("the override did not apply: %v", schemas)
	}
	if _, ok := local["properties"].(map[string]any)["handle"]; !ok {
		t.Errorf("LocalContact = %v", local)
	}
	if lib := schemas["Contact"].(map[string]any)["properties"].(map[string]any); len(lib) != 3 {
		t.Errorf("Contact = %v, want the library shape", lib)
	}
	props := schemas["nestedDup"].(map[string]any)["properties"].(map[string]any)
	if ref := props["local"].(map[string]any)["$ref"]; ref != "#/components/schemas/LocalContact" {
		t.Errorf("local ref = %v", ref)
	}
	if ref := props["lib"].(map[string]any)["$ref"]; ref != "#/components/schemas/Contact" {
		t.Errorf("lib ref = %v", ref)
	}
}
