// Package specout generates OpenAPI 3.1 specifications from plain
// http.HandlerFunc factories. Handlers stay raw std; metadata rides on the
// generic Handler[Req, Res] wrapper and is discovered at registration time.
package specout

import "sync"

// Generator assembles and serves an OpenAPI 3.1 document built from the
// routes registered on it. The spec builds lazily on first serve and freezes;
// registering after the freeze panics.
type Generator struct {
	cfg Config

	mu       sync.Mutex
	frozen   bool
	routes   *routeTable
	schemas  *schemaRegistry
	specJSON []byte
}

// New creates a Generator from cfg.
func New(cfg Config) *Generator {
	return &Generator{
		cfg:     cfg,
		routes:  newRouteTable(),
		schemas: newSchemaRegistry(),
	}
}
