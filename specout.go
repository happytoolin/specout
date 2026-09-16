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

	mu     sync.Mutex
	frozen bool
	// routes holds registration records keyed by handler func code pointer.
	// Closures from the same literal share a code pointer, sharing one key.
	routes map[uintptr][]*routeRecord
	// chiRoots: every distinct chi router registrations were made on.
	chiRoots []chiRouter
	// union variant registrations and component-name overrides
	variants      map[string]reflect.Type
	nameOverrides map[reflect.Type]string
	resolved      bool

	specJSON []byte
}

func New(cfg Config) *Generator {
	return &Generator{
		cfg:           cfg,
		routes:        make(map[uintptr][]*routeRecord),
		variants:      make(map[string]reflect.Type),
		nameOverrides: make(map[reflect.Type]string),
	}
}
