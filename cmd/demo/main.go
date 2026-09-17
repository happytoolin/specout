package main

import (
	"fmt"
	"net/http"

	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/happytoolin/specout/internal/examplekit"
)

// GO_SPEC_ONLY=1 ./demo prints the spec to stdout so CI can diff the golden.

// Scalar (/scalar) and Redoc (/redoc) share one skeleton: a title and a body
// that loads /openapi.json in the browser. The pages carry no template
// actions, so the text is served as-is.
const docPage = `<!DOCTYPE html>
<html>
<head>
  <title>specout demo — %s</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</head>
<body>
%s
</body>
</html>`

const scalarBody = `<script id="api-reference" data-url="/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>`

const redocBody = `<redoc spec-url="/openapi.json"></redoc>
<script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>`

func static(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	}
}

func main() {
	d, r := router.New()
	if examplekit.EmitSpec(d) {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/scalar", static(fmt.Sprintf(docPage, "Scalar", scalarBody)))
	mux.HandleFunc("/redoc", static(fmt.Sprintf(docPage, "Redoc", redocBody)))
	// Swagger UI at /; every other path falls through to the API router.
	mux.Handle("/", examplekit.Page(examplekit.SwaggerPage("specout demo — Swagger UI", "/openapi.json"), r))

	fmt.Println("swagger ui: http://localhost:8080/   scalar: http://localhost:8080/scalar   redoc: http://localhost:8080/redoc\nspec:        http://localhost:8080/openapi.json")
	http.ListenAndServe(":8080", mux)
}
