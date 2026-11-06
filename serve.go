package specout

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"

	json "encoding/json/v2"
)

// Build builds and freezes the spec, and returns it as indented JSON. It
// returns every problem that it finds. Call it at startup or in a test, so
// that a bad document fails there, and serve the bytes on a route of your
// router. A failed build does not freeze the spec.
func (g *Generator) Build() ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.specJSON == nil {
		b, err := g.encode()
		if err != nil {
			return nil, err
		}
		g.specJSON = b
	}
	return slices.Clone(g.specJSON), nil
}

// encode builds the spec and encodes it as indented JSON. Caller holds g.mu.
func (g *Generator) encode() ([]byte, error) {
	spec, err := g.buildDoc()
	if err != nil {
		return nil, err
	}
	// obj carries insertion order; jsontext preserves it. Indent directly.
	b, err := json.Marshal(spec, jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("specout: encode spec: %w", err)
	}
	if refErr := checkRefs(b); refErr != nil {
		return nil, refErr
	}
	return append(b, '\n'), nil // trailing newline, like a text file
}
