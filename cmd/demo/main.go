package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"

	"github.com/happytoolin/specout/internal/demo/router"
)

// GO_SPEC_ONLY=1 ./demo > openapi.json makes CI golden-diff possible from
// the same binary that serves.

// Swagger UI, served at /.
const swaggerPage = `<!DOCTYPE html>
<html>
<head>
  <title>specout demo — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  window.ui = SwaggerUIBundle({ url: '/openapi.json', dom_id: '#swagger-ui' })
</script>
</body>
</html>`

// Scalar, served at /scalar: the modern alternative, same spec.
const scalarPage = `<!DOCTYPE html>
<html>
<head>
  <title>specout demo — Scalar</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</head>
<body>
<script id="api-reference" data-url="/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`

func main() {
	d, r := router.New()

	if os.Getenv("GO_SPEC_ONLY") != "" {
		if err := d.WriteJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/scalar", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		template.Must(template.New("scalar").Parse(scalarPage)).Execute(w, nil)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			template.Must(template.New("swagger").Parse(swaggerPage)).Execute(w, nil)
			return
		}
		r.ServeHTTP(w, req)
	})
	fmt.Println("swagger ui: http://localhost:8080/       scalar: http://localhost:8080/scalar")
	fmt.Println("spec:        http://localhost:8080/openapi.json")
	http.ListenAndServe(":8080", mux)
}
