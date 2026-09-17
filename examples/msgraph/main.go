// Command msgraph serves eight operations of the published Microsoft Graph
// v1.0 OpenAPI description — the user resource plus /me — with specout types
// and gorilla/mux. Summaries, descriptions, operationIds, parameter names and
// response codes are the published ones; examples/README.md lists deviations.
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gorilla/mux"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit"
)

// graphDescription is info.description of the published document.
const graphDescription = "This OData service is located at https://graph.microsoft.com/v1.0"

// New builds the generator and the router. The published document declares no
// securitySchemes, though Graph takes a bearer token.
func New() (*specout.Generator, *mux.Router) {
	d := specout.New(specout.Config{
		Title:       "OData Service for namespace microsoft.graph",
		Version:     "v1.0",
		Description: graphDescription,
		Servers:     []specout.Server{{URL: "https://graph.microsoft.com/v1.0"}},
		Tags:        []specout.Tag{{Name: "me.user"}, {Name: "users.user"}, {Name: "users.user.Actions"}, {Name: "users.profilePhoto"}},
	})

	s := newStore()
	r := mux.NewRouter()
	b := specout.Gorilla(d, r)
	routes(b, s)
	if err := b.Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

// Handler serves the demo: Swagger UI at / and the spec at /openapi.json. The
// published server URL is absolute, so "try it out" aims at real Graph.
func Handler(d *specout.Generator, r http.Handler) http.Handler {
	std := http.NewServeMux()
	std.Handle("/openapi.json", d)
	std.Handle("/", examplekit.Page(examplekit.SwaggerPage("specout example — Microsoft Graph subset", "/openapi.json"), r))
	return std
}

func main() {
	d, r := New()
	if examplekit.EmitSpec(d) {
		return
	}
	fmt.Println("msgraph: http://localhost:8082/   spec: http://localhost:8082/openapi.json")
	if err := http.ListenAndServe(":8082", Handler(d, r)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
