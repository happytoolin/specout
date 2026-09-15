// Package router wires the demo service: generator config, route groups,
// subrouters, the spec mount, and Adopt for full-path resolution.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/handlers"
	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/internal/demo/webhooks"
)

// New builds the whole service. Returns the generator (for tests and CI
// export) and the http.Handler to serve.
func New() (*specout.Generator, http.Handler) {
	d := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "specout demo service — every route shape in one app.",
		ErrorType:     api.Problem{},
		DefaultErrors: []int{400, 401, 403, 404, 409, 500},
		Servers:       []specout.Server{{URL: "http://localhost:8080", Description: "development"}},
		Tags: []specout.Tag{
			{Name: "onboarding", Description: "Onboarding lifecycle"},
			{Name: "sync", Description: "Version sync"},
			{Name: "files", Description: "Import and export"},
			{Name: "webhooks", Description: "Outbound notifications"},
		},
	})

	// union variants — reflection can't discover interface implementations
	d.Register[webhooks.EmailConfig]("email")
	d.Register[webhooks.SlackConfig]("slack")

	deps := handlers.Deps{
		Store:  onboarding.NewStore(),
		Mapper: api.DefaultMapper,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/onboarding", func(r chi.Router) {
		d.Get(r, "/", handlers.HandleList(deps))
		d.Post(r, "/", handlers.HandleCreate(deps))

		r.Route("/{id}", func(r chi.Router) {
			d.Get(r, "/", handlers.HandleGet(deps))
			d.Put(r, "/", handlers.HandleUpsert(deps))
			d.Delete(r, "/", handlers.HandleDelete(deps))
			d.Post(r, "/sync", handlers.HandleSync(deps))
		})
	})

	d.Post(r, "/files/import", handlers.HandleImport(deps))
	d.Get(r, "/files/report", handlers.HandleReport(deps))
	d.Post(r, "/webhooks", handlers.HandleRegisterWebhook(deps))

	// deprecated endpoint kept for old clients — spec carries deprecated: true
	d.Get(r, "/legacy", handlers.HandleLegacyGet(deps))

	r.Mount("/openapi.json", d)

	// Adopt the root: Route/Mount prefixes compose into full paths at build.
	if err := d.Adopt(r); err != nil {
		panic(err)
	}
	return d, r
}
