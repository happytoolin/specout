// Package router wires the demo service: config, route groups, spec mount, Adopt.
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
)

// New returns the generator (for tests and CI export) and the handler to serve.
func New() (*specout.Generator, http.Handler) {
	d := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "specout demo service — every route shape in one app.",
		ErrorType:     api.Problem{},
		DefaultErrors: []int{400, 401, 403, 404, 409, 500},
		// any one of these satisfies auth (OR semantics in security:)
		Auth: []specout.AuthScheme{specout.Bearer, specout.APIKey("apiKey", "X-API-Key", specout.InHeader),
			specout.APIKey("session", "session", specout.InCookie)}, // Bearer | X-API-Key | session cookie
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
	d.Register[validators.EmailConfig]("email")
	d.Register[validators.SlackConfig]("slack")
	d.Register[validators.EmailChannel]("notify_email")
	d.Register[validators.SlackChannel]("notify_slack")

	deps := handlers.Deps{Store: onboarding.NewStore(), Mapper: api.DefaultMapper}

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	// Bind inside each group: a binder captured on the root registers on the
	// root, so the group's middleware would silently never run.
	r.Group(func(r chi.Router) {
		r.Use(auth.Require)
		rc := specout.Chi(d, r)
		rc.Post("/onboarding", handlers.HandleCreate(deps))
		rc.Put("/onboarding/{id}", handlers.HandleUpsert(deps))
		rc.Delete("/onboarding/{id}", handlers.HandleDelete(deps))
		rc.Post("/onboarding/{id}/sync", handlers.HandleSync(deps))
		rc.Post("/files/import", handlers.HandleImport(deps))
		rc.Post("/webhooks", handlers.HandleRegisterWebhook(deps))
		rc.Post("/things", handlers.HandleCreateThing())
		rc.Post("/channels", handlers.HandleConfigureChannel())
	})

	r.Group(func(r chi.Router) {
		rc := specout.Chi(d, r)
		rc.Get("/onboarding", handlers.HandleList(deps))
		rc.Get("/onboarding/{id}", handlers.HandleGet(deps))
		rc.Get("/files/report", handlers.HandleReport(deps))
		rc.Get("/legacy", handlers.HandleLegacyGet(deps))
		rc.Get("/things", handlers.HandleSearchThings())
	})

	r.Mount("/openapi.json", d)

	// Adopt the root: Route/Mount prefixes compose into full paths at build.
	if err := specout.Chi(d, r).Adopt(); err != nil {
		panic(err)
	}
	return d, r
}
