// Package examplekit is the plumbing the specout examples share. It is
// internal to this repo and is never part of the library: specout itself
// ships no runtime helpers.
package examplekit

import (
	"fmt"
	"net/http"
	"os"

	"github.com/happytoolin/specout"
)

// SwaggerPage is the Swagger UI page for a spec served at specPath.
func SwaggerPage(title, specPath string) string {
	return `<!DOCTYPE html>
<html>
<head>
  <title>` + title + `</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>window.ui = SwaggerUIBundle({ url: '` + specPath + `', dom_id: '#swagger-ui' })</script>
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
		fmt.Fprint(w, body)
	}
}

// EmitSpec handles GO_SPEC_ONLY: print the spec to stdout so CI can diff the
// golden, and report true so main returns without serving.
func EmitSpec(d *specout.Generator) bool {
	if os.Getenv("GO_SPEC_ONLY") == "" {
		return false
	}
	if err := d.WriteJSON(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}
