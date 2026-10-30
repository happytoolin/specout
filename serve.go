package specout

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"

	json "encoding/json/v2"

	"github.com/go-chi/chi/v5"
)

// ServeHTTP serves the built spec. GET (and HEAD) only; other methods get 405.
// A failed build gets a 500 that does not name routes or types. The error goes
// to the standard logger once per failed build; WriteJSON returns it. A panic
// in the build counts as a failed build here, and the log gets its stack;
// WriteJSON panics.
func (d *Generator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	spec, fresh, err := d.bytes(chiOuter(r), true)
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

// chiOuter returns the chi router that served r, or nil. bytes compares it with
// ==, so a chi.Routes value that Go cannot compare is not used.
func chiOuter(r *http.Request) routeSource {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.Routes == nil || !canCompare(rctx.Routes) {
		return nil
	}
	return chiWalkSource{rctx.Routes}
}

// Build builds and freezes the spec, and returns every problem it finds. Call it
// at startup or in a test, so that a bad document fails there and not on the
// first request. A panic in the build reaches the caller.
func (d *Generator) Build() error {
	_, _, err := d.bytes(nil, false)
	return err
}

// WriteJSON writes the spec to w. Builds and freezes if not already.
func (d *Generator) WriteJSON(w io.Writer) error {
	b, _, err := d.bytes(nil, false)
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
// nil, is the chi router that served the spec request. With catch, a panic in
// the build is kept as its error; without it, a kept panic does not count, so
// the build runs again and the panic reaches the caller.
func (d *Generator) bytes(outer routeSource, catch bool) ([]byte, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.specJSON != nil {
		return d.specJSON, false, nil
	}
	if outer != nil && outer != d.outer {
		d.outer = outer
		d.inputs++
	}
	if d.buildErr != nil && d.errAt == d.inputs && (catch || !isBuildPanic(d.buildErr)) {
		return nil, false, d.buildErr
	}
	encode := d.encode
	if catch {
		encode = d.encodeCaught
	}
	b, err := encode()
	if err != nil {
		d.buildErr, d.errAt = err, d.inputs
		return nil, true, err
	}
	d.specJSON, d.buildErr = b, nil
	return b, false, nil
}

// buildPanicError is a panic in a build that ServeHTTP ran, kept as the build
// error.
type buildPanicError struct {
	value any
	stack []byte
}

func (p *buildPanicError) Error() string {
	return fmt.Sprintf("specout: spec build panicked: %v\n%s", p.value, p.stack)
}

// isBuildPanic reports whether err is a kept build panic.
func isBuildPanic(err error) bool {
	var p *buildPanicError
	return errors.As(err, &p)
}

// encodeCaught runs encode and returns a panic in it as a *buildPanicError.
// Caller holds d.mu.
func (d *Generator) encodeCaught() ([]byte, error) {
	var b []byte
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				err = &buildPanicError{value: v, stack: debug.Stack()}
			}
		}()
		b, err = d.encode()
	}()
	return b, err
}

// encode builds the spec and encodes it as indented JSON. Caller holds d.mu.
func (d *Generator) encode() ([]byte, error) {
	spec, err := d.buildDoc()
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
