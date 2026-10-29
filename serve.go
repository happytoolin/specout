package specout

import (
	"encoding/json/jsontext"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	json "encoding/json/v2"

	"github.com/go-chi/chi/v5"
)

// ServeHTTP serves the built spec. GET (and HEAD) only; other methods get 405.
// A failed build gets a 500 that does not name routes or types. The error goes
// to the standard logger once per failed build; WriteJSON returns it.
func (d *Generator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var outer routeSource
	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.Routes != nil {
		outer = chiWalkSource{rctx.Routes}
	}
	spec, fresh, err := d.bytes(outer)
	if err != nil {
		if fresh {
			log.Print(err)
		}
		http.Error(w, "specout: spec build failed; see the server log", http.StatusInternalServerError)
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
	b, _, err := d.bytes(nil)
	if err != nil {
		return err
	}
	if _, err = w.Write(b); err != nil {
		return fmt.Errorf("specout: write spec: %w", err)
	}
	return nil
}

// bytes returns the frozen spec bytes, building lazily on first call. A failed
// build is not run again until a registration, Adopt or other input change, so
// a broken spec costs one build, not one per request. The bool reports an
// error from a build that ran in this call. A route added to an adopted router
// after a failed build is an input change specout cannot see. outer, when not
// nil, is the chi router that served the spec request.
func (d *Generator) bytes(outer routeSource) ([]byte, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.specJSON != nil {
		return d.specJSON, false, nil
	}
	if outer != nil && outer != d.outer {
		d.outer = outer
		d.inputs++
	}
	if d.buildErr != nil && d.errAt == d.inputs {
		return nil, false, d.buildErr
	}
	b, err := d.encode()
	if err != nil {
		d.buildErr, d.errAt = err, d.inputs
		return nil, true, err
	}
	d.specJSON, d.buildErr = b, nil
	return b, false, nil
}

// encode builds the spec and encodes it as indented JSON. Caller holds d.mu.
func (d *Generator) encode() ([]byte, error) {
	spec, err := d.build()
	if err != nil {
		return nil, err
	}
	// obj carries insertion order; jsontext preserves it. Indent directly.
	b, err := json.Marshal(spec, jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("specout: encode spec: %w", err)
	}
	return append(b, '\n'), nil // trailing newline, like a text file
}
