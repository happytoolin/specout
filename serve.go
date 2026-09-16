package specout

import (
	"io"
	"net/http"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// ServeHTTP serves the built spec. GET (and HEAD) only; other methods get 405.
func (d *Generator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	spec, err := d.bytes()
	if err != nil {
		http.Error(w, "spec build failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(spec)
}

// WriteJSON writes the spec to w. Builds and freezes if not already.
func (d *Generator) WriteJSON(w io.Writer) error {
	b, err := d.bytes()
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
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
		return nil, err
	}
	d.frozen = true
	d.specJSON = append(b, '\n') // trailing newline, like a text file
	return d.specJSON, nil
}
