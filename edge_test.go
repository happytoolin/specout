package specout_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

func dump(t *testing.T, name string, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	if r != nil {
		r.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	} else {
		d.WriteJSON(&strings.Builder{})
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("%s: invalid json: %v", name, err)
	}
	fmt.Printf("=== %s ===\n%s\n", name, w.Body.String())
	return doc
}

// recursive type
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

type Alpha struct{ V string }
type Beta struct{ W string }

type SliceQueryReq struct {
	Tags []string `query:"tags"`
	Ids  []int    `query:"ids"`
}

type StructQueryReq struct {
	Filter struct{ Min int } `query:"filter"`
	Name   string            `json:"name"`
}

type BothTagReq struct {
	Limit int `query:"limit" json:"limit"`
}

type UnregisteredUnionReq struct {
	Kind string `json:"kind" jsonschema:"enum=a|b,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=a|b"`
}

type WildcardReq struct {
	Rest string `query:"rest"`
}

func noop(w http.ResponseWriter, r *http.Request) {}

func TestEdgeCases(t *testing.T) {
	t.Run("recursive", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/tree", specout.Handler[struct{}, Node]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		doc := dump(t, "recursive", d, r)
		_ = doc
	})
	t.Run("maps", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/m", specout.Handler[struct{}, MapWrap]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		dump(t, "maps", d, r)
	})
	t.Run("generic collision", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/a", specout.Handler[struct{}, Page[Alpha]]{HandlerFunc: noop})
		d.Get(r, "/b", specout.Handler[struct{}, Page[Beta]]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		doc := dump(t, "generic-collision", d, r)
		comps := doc["components"].(map[string]any)["schemas"].(map[string]any)
		t.Logf("components: %v", keys(comps))
	})
	t.Run("slice query params", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/q", specout.Handler[SliceQueryReq, struct{}]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		dump(t, "slice-query", d, r)
	})
	t.Run("struct query param", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/q", specout.Handler[StructQueryReq, struct{}]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		dump(t, "struct-query", d, r)
	})
	t.Run("query+json both", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Post(r, "/q", specout.Handler[BothTagReq, struct{}]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		dump(t, "both-tags", d, r)
	})
	t.Run("unregistered union", func(t *testing.T) {
		defer func() {
			if p := recover(); p != nil {
				t.Logf("PANICKED: %v", p)
			}
		}()
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Post(r, "/u", specout.Handler[UnregisteredUnionReq, struct{}]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		dump(t, "unregistered-union", d, r)
	})
	t.Run("public without auth", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/p", specout.Handler[struct{}, Alpha]{HandlerFunc: noop, Public: true})
		r.Mount("/openapi.json", d)
		dump(t, "public-no-auth", d, r)
	})
	t.Run("weird statuses", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/s", specout.Handler[struct{}, Alpha]{
			HandlerFunc: noop,
			Responses: []specout.Response{
				{Status: 304},
				{Status: 599},
				{Status: 0},
			},
		})
		r.Mount("/openapi.json", d)
		dump(t, "weird-statuses", d, r)
	})
	t.Run("path wildcard param", func(t *testing.T) {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/files/{path...}", specout.Handler[WildcardReq, struct{}]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		dump(t, "wildcard", d, r)
	})
	t.Run("duplicate param name", func(t *testing.T) {
		type DupReq struct {
			ID string `query:"id"`
		}
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		d.Get(r, "/things/{id}", specout.Handler[DupReq, struct{}]{HandlerFunc: noop})
		r.Mount("/openapi.json", d)
		doc := dump(t, "dup-param", d, r)
		get := doc["paths"].(map[string]any)["/things/{id}"].(map[string]any)["get"].(map[string]any)
		params := get["parameters"].([]any)
		t.Logf("params: %d", len(params))
	})
}

func keys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
