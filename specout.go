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

	// records, in registration order. Identity is the record itself, not
	// the handler func pointer: one func on two routes is two records.
	records []*routeRecord
	// sources: every router the generator saw, walked once at build time.
	sources []routeSource
	// known handlers: func code pointer -> true, for stray detection.
	known map[uintptr]bool

	variants      map[string]reflect.Type
	nameOverrides map[reflect.Type]string

	specJSON []byte
}

func New(cfg Config) *Generator {
	return &Generator{
		cfg:           cfg,
		known:         make(map[uintptr]bool),
		variants:      make(map[string]reflect.Type),
		nameOverrides: make(map[reflect.Type]string),
	}
}
