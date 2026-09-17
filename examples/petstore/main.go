// Command petstore rebuilds the published Swagger Petstore definition
// (OpenAPI 3.0.4, 19 operations, 13 paths) with specout types, and serves it
// with chi. Every shape in the published document is carried over in the
// types below; examples/README.md lists the handful that are not.
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit"
)

// petstoreDescription is info.description of the published document.
const petstoreDescription = `This is a sample Pet Store Server based on the OpenAPI 3.0 specification. You can find out more about Swagger at [https://swagger.io](https://swagger.io). In the third iteration of the pet store, we've switched to the design first approach!
You can now help us improve the API whether it's by making changes to the definition itself or to the code.

Some useful links:
- [The Pet Store repository](https://github.com/swagger-api/swagger-petstore)
- [The source API definition for the Pet Store](https://github.com/swagger-api/swagger-petstore/blob/master/src/main/resources/openapi.yaml)`

// New builds the service: the generator, and the chi router that serves it.
func New() (*specout.Generator, http.Handler) {
	d := specout.New(specout.Config{
		Title:       "Swagger Petstore - OpenAPI 3.0",
		Version:     "1.0.27",
		Description: petstoreDescription,
		Servers:     []specout.Server{{URL: "/api/v3"}},
		// the published pair: an api key in the header, and the petstore's
		// own oauth2 scheme with its two pet scopes.
		Auth: []specout.AuthScheme{
			specout.APIKey("api_key", "api_key", specout.InHeader),
			specout.OAuth2("petstore_auth", map[string]specout.OAuth2Flow{
				"implicit": {
					AuthorizationURL: "https://petstore3.swagger.io/oauth/authorize",
					Scopes: map[string]string{
						"write:pets": "modify pets in your account",
						"read:pets":  "read your pets",
					},
				},
			}),
		},
		ExternalDocs:   &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more about Swagger"},
		TermsOfService: "https://swagger.io/terms/",
		Contact:        &specout.Contact{Email: "apiteam@swagger.io"},
		License: &specout.License{
			Name: "Apache 2.0",
			URL:  "https://www.apache.org/licenses/LICENSE-2.0.html",
		},
		Tags: []specout.Tag{
			{Name: "pet", Description: "Everything about your Pets",
				ExternalDocs: &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more"}},
			{Name: "store", Description: "Access to Petstore orders",
				ExternalDocs: &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more about our store"}},
			{Name: "user", Description: "Operations about user"},
		},
	})

	s := newStore()
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)

	// One binder on the root router with the published absolute patterns: a
	// group root would document "/pet/" where the published document has "/pet".
	b := specout.Chi(d, r)
	petRoutes(b, s)
	storeRoutes(b, s)
	userRoutes(b, s)

	r.Mount("/openapi.json", d)
	// Adopt registers the build-time walk and fails on undocumented routes.
	if err := b.Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

// Handler wraps the chi router with the demo page and the published server
// prefix: the document declares servers: [/api/v3], so the API is served
// there and the Swagger UI "try it out" button hits real routes.
func Handler(d *specout.Generator, r http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v3/", http.StripPrefix("/api/v3", r))
	page := examplekit.SwaggerPage("specout example — Swagger Petstore", "/api/v3/openapi.json")
	mux.HandleFunc("/", examplekit.Page(page, http.NotFoundHandler()))
	return mux
}

func main() {
	d, r := New()
	if examplekit.EmitSpec(d) {
		return
	}
	fmt.Println("petstore: http://localhost:8081/   spec: http://localhost:8081/api/v3/openapi.json")
	if err := http.ListenAndServe(":8081", Handler(d, r)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
