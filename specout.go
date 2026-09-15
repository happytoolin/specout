// Package specout generates OpenAPI 3.1 specifications from plain
package specout

import (
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

	specJSON []byte
}

func New(cfg Config) *Generator {
	// ponytail: single walk root assumed; multiple mounted roots need a
	// build-time walk of each and path-prefixing (same as r.Mount).
	return &Generator{cfg: cfg, routes: make(map[uintptr][]*routeRecord)}
}
