// Package router wires the demo service: generator config, route groups,
// subrouters, the spec mount, and Adopt for full-path resolution.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/auth"
	"github.com/happytoolin/specout/internal/demo/handlers"
	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/internal/demo/validators"
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
		// any one of these satisfies auth (OR semantics in security:)
		Auth: []specout.AuthScheme{
			specout.Bearer, // Authorization: Bearer <token>
			specout.APIKey("apiKey", "X-API-Key", specout.InHeader), // X-API-Key header
			specout.APIKey("session", "session", specout.InCookie),  // session cookie
		},
		ExternalDocs: &specout.ExternalDocs{URL: "https://docs.example.com/onboarding", Description: "Full guides"},
		Servers:      []specout.Server{{URL: "http://localhost:8080", Description: "development"}},
		Tags: []specout.Tag{
			{Name: "onboarding", Description: "Onboarding lifecycle"},
			{Name: "sync", Description: "Version sync"},
			{Name: "files", Description: "Import and export"},
			{Name: "webhooks", Description: "Outbound notifications"},
			{Name: "validators", Description: "Constraint keyword showcase"},
		},
	})

	// union variants — reflection can't discover interface implementations
	d.Register[webhooks.EmailConfig]("email")
	d.Register[webhooks.SlackConfig]("slack")
	d.Register[validators.EmailChannel]("notify_email")
	d.Register[validators.SlackChannel]("notify_slack")

	deps := handlers.Deps{
		Store:  onboarding.NewStore(),
		Mapper: api.DefaultMapper,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// enforcement is app code; the spec only declares the schemes
	r.Group(func(r chi.Router) {
		r.Use(auth.Require)
		d.Post(r, "/onboarding", handlers.HandleCreate(deps))
		d.Put(r, "/onboarding/{id}", handlers.HandleUpsert(deps))
		d.Delete(r, "/onboarding/{id}", handlers.HandleDelete(deps))
		d.Post(r, "/onboarding/{id}/sync", handlers.HandleSync(deps))
		d.Post(r, "/files/import", handlers.HandleImport(deps))
		d.Post(r, "/webhooks", handlers.HandleRegisterWebhook(deps))
		d.Post(r, "/things", handlers.HandleCreateThing())
		d.Post(r, "/channels", handlers.HandleConfigureChannel())
	})

	// public: no credentials needed
	r.Group(func(r chi.Router) {
		d.Get(r, "/onboarding", handlers.HandleList(deps))
		d.Get(r, "/onboarding/{id}", handlers.HandleGet(deps))
		d.Get(r, "/files/report", handlers.HandleReport(deps))
		d.Get(r, "/legacy", handlers.HandleLegacyGet(deps))
		d.Get(r, "/things", handlers.HandleSearchThings())
	})

	r.Mount("/openapi.json", d)

	// Adopt the root: Route/Mount prefixes compose into full paths at build.
	if err := d.Adopt(r); err != nil {
		panic(err)
	}
	return d, r
}
