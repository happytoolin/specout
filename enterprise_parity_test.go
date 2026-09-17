package specout_test

import (
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recA and recB recurse through each other; package-level because local type
// declarations are scoped from the point of declaration, so neither can name
// the other.
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

// Regression: nested named types hoisted to $defs must keep jsonschema
// fixups (enum split, readonly, nullable) and must not overwrite the
// properly-reflected component registered later under the same name.
func TestNestedDefsKeepFixups(t *testing.T) {
	type Label struct {
		Color string `json:"color" jsonschema:"pattern=^[0-9a-f]{6}$"`
	}
	type Issue struct {
		ID    int64      `json:"id" jsonschema:"readonly"`
		State string     `json:"state" jsonschema:"enum=open|closed,default=open"`
		Label Label      `json:"label"`
		Due   *time.Time `json:"due,omitempty"`
	}
	type Page struct {
		Items []Issue `json:"items"`
	}

	doc := chiDoc(t, func(d *specout.Generator, r chi.Router) {
		specout.Chi(d, r).Get("/issues", specout.Handler[struct{}, Page]{HandlerFunc: noop})
		specout.Chi(d, r).Post("/issues", specout.Handler[struct {
			Title string `json:"title"`
		}, Issue]{HandlerFunc: noop})
	})
	p := props(t, doc, "Issue")
	require.Equal(t, []any{"open", "closed"}, p["state"].(map[string]any)["enum"], "state enum")
	assert.Equal(t, true, p["id"].(map[string]any)["readOnly"], "id readOnly lost")
	assert.True(t, isNullable(p["due"].(map[string]any)), "due wants [string null]")
}

// Recursive types reach themselves through a $ref. The fixup walk must not
// loop, and each component must still get its pass, including when two types
// recurse through each other.
func TestRecursiveNestedDefsFixed(t *testing.T) {
	type Node struct {
		Name string `json:"name"`
		Next *Node  `json:"next,omitempty"`
	}
	type Tree struct {
		Root Node `json:"root"`
	}
	doc := chiDoc(t, func(d *specout.Generator, r chi.Router) {
		specout.Chi(d, r).Get("/tree", specout.Handler[struct{}, Tree]{HandlerFunc: noop})
		specout.Chi(d, r).Get("/pair", specout.Handler[struct{}, recPair]{HandlerFunc: noop})
	})
	for _, c := range []struct{ name, prop string }{
		{"Node", "next"}, {"recA", "b"}, {"recA", "id"}, {"recB", "a"},
	} {
		assert.True(t, isNullable(props(t, doc, c.name)[c.prop].(map[string]any)), "%s.%s has no null arm", c.name, c.prop)
	}
}

// Regression: a nested-only type — never a top-level Req or Res — is hoisted
// to $defs before the fixup pass runs. The pass must follow the $ref into
// that component: without the walk the whole nested type keeps invopop's raw
// output, so its pointer fields are not nullable and its readonly/deprecated
// tags vanish. TestNestedDefsKeepFixups cannot catch this: it registers Issue
// top-level as well, and byType wins the name clash.
func TestNestedOnlyDefsKeepFixups(t *testing.T) {
	type Inner struct {
		ID   int64   `json:"id" jsonschema:"readonly"`
		Note string  `json:"note" jsonschema:"deprecated"`
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

// Regression: query params must carry jsonschema keywords from the field
// tag (enum, min/max, default), not just the bare type.
func TestParamKeywords(t *testing.T) {
	type ListReq struct {
		Limit int    `query:"limit" jsonschema:"default=20,minimum=1,maximum=100"`
		Sort  string `query:"sort" jsonschema:"enum=created|updated,default=created"`
	}
	doc := docOf(t, "GET", "/items", specout.Handler[ListReq, specout.NoContent]{HandlerFunc: noop})
	p := paramsOf(t, opOf(t, doc, "/items", "get"))

	limit := p["limit"]["schema"].(map[string]any)
	assert.Equal(t, 1.0, limit["minimum"], "limit minimum")
	assert.Equal(t, 100.0, limit["maximum"], "limit maximum")
	assert.Equal(t, 20.0, limit["default"], "limit default")
	assert.Equal(t, []any{"created", "updated"}, p["sort"]["schema"].(map[string]any)["enum"], "sort enum")
}

// Regression: anonymous Req/Res struct types must get clean deterministic
// component names, not the raw Go struct literal.
func TestAnonymousComponentNames(t *testing.T) {
	doc := chiDoc(t, func(d *specout.Generator, r chi.Router) {
		specout.Chi(d, r).Post("/a", specout.Handler[struct {
			A string `json:"a"`
		}, specout.NoContent]{HandlerFunc: noop})
		specout.Chi(d, r).Post("/b", specout.Handler[struct {
			B string `json:"b"`
		}, specout.NoContent]{HandlerFunc: noop})
	})
	for name := range schemas(t, doc) {
		assert.NotRegexp(t, "^struct", name, "raw struct literal leaked as component name")
	}
}
