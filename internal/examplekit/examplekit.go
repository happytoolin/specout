// Package examplekit is the plumbing the specout examples share. It is
// internal to this repo and is never part of the library: specout itself
// ships no runtime helpers.
package examplekit

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/happytoolin/specout"
)

// DecodeJSON decodes exactly one JSON value from r into v. Trailing data is
// an error.
func DecodeJSON(r io.Reader, v any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

// WriteJSON writes v as an application/json response with status code.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

// SwaggerPage is the Swagger UI page for a spec served at specPath.
func SwaggerPage(title, specPath string) string {
	return `<!DOCTYPE html>
<html>
<head>
  <title>` + html.EscapeString(title) + `</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script>window.ui = SwaggerUIBundle({ url: ` + strconv.Quote(specPath) + `, dom_id: '#swagger-ui' })</script>
</body>
</html>`
}

// Page serves body as text/html at "/" and hands every other path to next.
func Page(body string, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, body)
	}
}

// Spec builds the document of d and returns a handler that serves it. Call it
// after every route is registered: the build freezes d. A bad document
// panics, so the example fails at startup.
func Spec(d *specout.Generator) http.HandlerFunc {
	spec, err := d.Build()
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	}
}

// EmitSpec handles GO_SPEC_ONLY: print the spec to stdout so CI can diff the
// golden, and report true so main returns without serving.
func EmitSpec(d *specout.Generator) bool {
	if os.Getenv("GO_SPEC_ONLY") == "" {
		return false
	}
	spec, err := d.Build()
	if err == nil {
		_, err = os.Stdout.Write(spec)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}

// Run exports the document for CI or starts one example server.
func Run(addr, name string, d *specout.Generator, handler http.Handler) {
	if EmitSpec(d) {
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s: http://localhost%s/\n", name, addr)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: ReadHeaderTimeout}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ReadHeaderTimeout is the header timeout the example binaries set:
// a stalled client cannot hold a connection open forever.
const ReadHeaderTimeout = 5 * time.Second
