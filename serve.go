package specout

import (
	"bytes"
	"io"
	"net/http"
	"reflect"

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

// isSelf reports whether handler is (wrapped) d — used to skip the spec's
// own mount when adopting a router.
func isSelf(d *Generator, handler http.Handler) bool {
	if g, ok := handler.(*Generator); ok {
		return g == d
	}
	// chi wraps mounts in a closure; compare the endpoint func pointer
	return reflect.ValueOf(handler).Pointer() == reflect.ValueOf(d.ServeHTTP).Pointer()
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
	// ponytail: json/v2 sorts map keys, giving byte-deterministic output
	// without an ordered-map type; swap in one if key order ever matters.
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	enc := jsontext.NewEncoder(&buf, jsontext.WithIndent("  "))
	for {
		val, err := dec.ReadValue()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := enc.WriteValue(val); err != nil {
			return nil, err
		}
	}
	d.frozen = true
	d.specJSON = buf.Bytes()
	return d.specJSON, nil
}
