// Package specout generates OpenAPI 3.1 specifications from plain Go
// http.HandlerFunc factories: metadata rides the generic Handler type,
// the spec builds from the live router, and the output is
// byte-deterministic.
package specout

import (
	"reflect"
	"sync"
)

// Generator assembles and serves an OpenAPI 3.1 document built from the
// routes registered on it. The spec builds lazily on first serve and freezes;
// registering after the freeze panics.
type Generator struct {
	cfg Config

	mu sync.Mutex

	// records, in registration order. Identity is the record itself, not
	// the handler func pointer: one func on two routes is two records.
	records []*routeRecord
	// sources: every router the generator saw, walked once at build time.
	sources []routeSource

	variants      map[string]reflect.Type
	nameOverrides map[reflect.Type]string

	specJSON []byte
}

// New returns a Generator that collects routes for cfg. It is safe for
// concurrent registration; the spec freezes on first read.
func New(cfg Config) *Generator {
	return &Generator{
		cfg:           cfg,
		variants:      make(map[string]reflect.Type),
		nameOverrides: make(map[reflect.Type]string),
	}
}

// mustBeOpen panics when the spec was already built, so no registration can
// slip into a frozen document. Caller holds d.mu.
func (d *Generator) mustBeOpen() {
	if d.specJSON != nil {
		panic("specout: registration after the spec was built")
	}
}

// Register declares T a union variant under name, for oneof_type tags on
// Variant discriminator fields. Call before building the spec.
func (d *Generator) Register[T any](name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mustBeOpen()
	d.variants[name] = reflect.TypeFor[T]()
}

// SchemaName overrides the component name generated for T — the fix when two
// packages own types with the same name. Call before building the spec.
// Names must be nonempty and contain only letters, digits, dots, hyphens or
// underscores. T and *T name the same component; the last override wins.
func (d *Generator) SchemaName[T any](name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mustBeOpen()
	if name == "" || componentChars.MatchString(name) {
		panic("specout: SchemaName must contain only ASCII letters, digits, dots, hyphens or underscores")
	}
	d.nameOverrides[deref(reflect.TypeFor[T]())] = name
}
