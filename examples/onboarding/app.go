// Package main serves a compact onboarding API and its generated OpenAPI document.
package main

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit"
)

func New() (*specout.Generator, http.Handler) {
	d := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "A compact specout example.",
		ErrorType:     problem{},
		DefaultErrors: []int{400, 401, 404, 500},
		Auth: []specout.AuthScheme{
			specout.Bearer,
			specout.APIKey("apiKey", "X-API-Key", specout.InHeader),
		},
		Servers: []specout.Server{{URL: "http://localhost:8080"}},
		Tags:    []specout.Tag{{Name: "onboarding", Description: "Onboarding lifecycle"}},
	})
	d.Register[emailWebhook]("email")
	d.Register[slackWebhook]("slack")

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(requireAuth)
		routes(specout.Chi(d, r), newStore())
	})
	r.Mount("/openapi.json", d)
	if err := specout.Chi(d, r).Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Method != http.MethodGet && (!bearer || strings.TrimSpace(token) == "") && strings.TrimSpace(r.Header.Get("X-API-Key")) == "" {
			writeProblem(w, http.StatusUnauthorized, "missing credentials")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Handler(api http.Handler) http.Handler {
	return examplekit.Page(examplekit.SwaggerPage("specout — onboarding", "/openapi.json"), api)
}
