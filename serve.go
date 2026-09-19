package specout

import (
	"encoding/json/jsontext"
	"fmt"
	"io"
	"net/http"
	"strconv"

	json "encoding/json/v2"
)

// ServeHTTP serves the built spec. GET (and HEAD) only; other methods get 405.
func (d *Generator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	spec, err := d.bytes()
	if err != nil {
		http.Error(w, "spec build failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(spec)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(spec)
	}
}

// WriteJSON writes the spec to w. Builds and freezes if not already.
func (d *Generator) WriteJSON(w io.Writer) error {
	b, err := d.bytes()
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("specout: write spec: %w", err)
	}
	return nil
}

// bytes returns the frozen spec bytes, building lazily on first call.
func (d *Generator) bytes() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.specJSON != nil {
		return d.specJSON, nil
	}
	spec, err := d.build()
	if err != nil {
		return nil, err
	}
	// obj carries insertion order; jsontext preserves it. Indent directly.
	b, err := json.Marshal(spec, jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("specout: encode spec: %w", err)
	}
	d.frozen = true
	b = append(b, '\n') // trailing newline, like a text file
	d.specJSON = b
	return d.specJSON, nil
}
