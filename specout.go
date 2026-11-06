// Package specout generates an OpenAPI 3.1 document from Go types and plain
// net/http handlers.
//
// A Handler[Req, Res] holds one route: its http.HandlerFunc, the Req type of
// its parameters and body, the Res type of its success body, and the other
// fields of the operation. Register it on a router with Bind and the adapter
// of the router, or record it with Document. Build then builds the document.
// The output is the same for the same routes.
package specout

import (
	"reflect"
	"strconv"
	"sync"
)

// Generator assembles an OpenAPI 3.1 document from the routes registered on
// it. Build builds the spec and freezes it; registering after the freeze
// panics.
type Generator struct {
	cfg Config

	mu sync.Mutex

	// records, in registration order. Identity is the record itself, not
	// the handler func pointer: one func on two routes is two records.
	records []*routeRecord
	// webhooks are requests the API sends. They are not routes, so they stay
	// out of records and of every route check.
	webhooks []webhook

	variants      map[string]reflect.Type
	nameOverrides map[reflect.Type]string

	specJSON []byte
}

// New returns a Generator that collects routes for cfg. It is safe for
// concurrent registration; the spec freezes on the first Build. A cfg that would
// make the document invalid panics here.
func New(cfg Config) *Generator {
	g := &Generator{
		cfg:           cfg,
		variants:      make(map[string]reflect.Type),
		nameOverrides: make(map[reflect.Type]string),
	}
	g.checkConfig()
	return g
}

// beginChange panics when the spec was already built, so no registration can
// change a frozen document. Caller holds g.mu.
func (g *Generator) beginChange() {
	if g.specJSON != nil {
		panic("specout: cannot register after the spec was built; " +
			"register all routes before Build")
	}
}

// Variant declares T a union variant under name, for the oneof_type tag of a
// discriminator field. Call it before the spec is built.
func (g *Generator) Variant[T any](name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beginChange()
	g.variants[name] = reflect.TypeFor[T]()
}

// SchemaName overrides the component name generated for T. Use it when two
// packages have types with the same name. Call before building the spec.
// Names must be nonempty and contain only letters, digits, dots, hyphens or
// underscores. T and *T name the same component; the last override wins.
func (g *Generator) SchemaName[T any](name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beginChange()
	if name == "" || componentChars.MatchString(name) {
		panic("specout: SchemaName " + strconv.Quote(name) +
			" must contain only ASCII letters, digits, dots, hyphens or underscores")
	}
	g.nameOverrides[deref(reflect.TypeFor[T]())] = name
}
