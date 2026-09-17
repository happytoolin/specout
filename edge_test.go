package specout_test

import (
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
)

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

// TestEdgeCases builds one document per awkward Req or Res shape: each case
// must build without panicking. Two cases also assert the shape they guard.
func TestEdgeCases(t *testing.T) {
	type testCase struct {
		name  string
		route func(*specout.Generator)
		check func(*testing.T, map[string]any)
	}
	cases := []testCase{
		{name: "recursive", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/tree", specout.Handler[struct{}, Node]{HandlerFunc: noop})
		}},
		{name: "maps", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/m", specout.Handler[struct{}, MapWrap]{HandlerFunc: noop})
		}},
		{
			name: "generic collision",
			route: func(d *specout.Generator) {
				specout.Document(d, http.MethodGet, "/a", specout.Handler[struct{}, Page[Alpha]]{HandlerFunc: noop})
				specout.Document(d, http.MethodPost, "/b", specout.Handler[struct{}, Page[Beta]]{HandlerFunc: noop})
			},
			check: func(t *testing.T, doc map[string]any) {
				// two instantiations of one generic type are two components
				for _, want := range []string{"PageAlpha", "PageBeta"} {
					assert.Contains(t, schemas(t, doc), want, "missing component")
				}
			},
		},
		{name: "slice query params", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/q", specout.Handler[SliceQueryReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "struct query param", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/q", specout.Handler[StructQueryReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "query+json both", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodPost, "/q", specout.Handler[BothTagReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "unregistered union", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodPost, "/u", specout.Handler[UnregisteredUnionReq, struct{}]{HandlerFunc: noop})
		}},
		{name: "public without auth", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/p", specout.Handler[struct{}, Alpha]{HandlerFunc: noop, Public: true})
		}},
		{name: "weird statuses", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/s", specout.Handler[struct{}, Alpha]{
				HandlerFunc: noop,
				Responses:   []specout.Response{{Status: 304}, {Status: 599}, {Status: 0}},
			})
		}},
		{name: "path wildcard param", route: func(d *specout.Generator) {
			specout.Document(d, http.MethodGet, "/files/{path...}", specout.Handler[WildcardReq, struct{}]{HandlerFunc: noop})
		}},
		{
			name: "duplicate param name",
			route: func(d *specout.Generator) {
				type DupReq struct {
					ID string `query:"id"`
				}
				specout.Document(d, http.MethodGet, "/things/{id}", specout.Handler[DupReq, struct{}]{HandlerFunc: noop})
			},
			check: func(t *testing.T, doc map[string]any) {
				// the path {id} and the query id are two parameters, not one,
				// so count the raw array: paramsOf indexes by name.
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
