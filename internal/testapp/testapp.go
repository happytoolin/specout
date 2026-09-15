package testapp

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

// Problem is the default error shape (RFC 9457 style).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Onboarding is the sample resource.
type Onboarding struct {
	ID    string `json:"id"`
	Owner string `json:"owner" jsonschema:"format=email"`
	Stage string `json:"stage" jsonschema:"enum=draft|active|archived"`
}

// UpsertRequest is the PUT/POST body.
type UpsertRequest struct {
	Owner string `json:"owner"`
	Stage string `json:"stage"`
}

// Empty marks a request with no body.
type Empty struct{}

func ok(w http.ResponseWriter, r *http.Request)        {}
func created(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(http.StatusCreated) }
func noContent(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }

// New builds the sample generator + router: groups, nested groups, a
// mounted subrouter, and the spec mounted at /openapi.json.
func New() (*specout.Generator, http.Handler) {
	d := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "Internal onboarding service.",
		ErrorType:     Problem{},
		DefaultErrors: []int{400, 404, 500},
	})

	r := chi.NewRouter()
	r.Route("/onboarding", func(r chi.Router) {
		d.Get(r, "/", specout.Handler[Empty, []Onboarding]{HandlerFunc: ok, Summary: "List onboarding", Tags: []string{"onboarding"}})
		d.Post(r, "/", specout.Handler[UpsertRequest, Onboarding]{
			HandlerFunc: created,
			Tags:        []string{"onboarding"},
			Responses:   []specout.Response{{Status: http.StatusCreated}},
		})
		r.Route("/{id}", func(r chi.Router) {
			d.Get(r, "/", specout.Handler[Empty, Onboarding]{HandlerFunc: ok, Tags: []string{"onboarding"}})
			d.Put(r, "/", specout.Handler[UpsertRequest, Onboarding]{
				HandlerFunc: ok,
				Tags:        []string{"onboarding"},
				Responses:   []specout.Response{{Status: http.StatusCreated}},
			})
			d.Delete(r, "/", specout.Handler[Empty, specout.NoContent]{HandlerFunc: noContent, Tags: []string{"onboarding"}})
		})
	})

	sub := chi.NewRouter()
	d.Post(sub, "/sync", specout.Handler[Empty, Onboarding]{HandlerFunc: ok, Tags: []string{"sync"}})
	r.Mount("/api", sub)

	r.Mount("/openapi.json", d)
	// Adopt the root so Route/Mount prefixes compose into full paths.
	if err := d.Adopt(r); err != nil {
		panic(err)
	}
	return d, r
}

// WriteSpec writes the built spec — the golden-fixture source.
func WriteSpec(w io.Writer) error {
	d, _ := New()
	return d.WriteJSON(w)
}
