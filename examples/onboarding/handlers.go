package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit"
)

type store struct {
	mu   sync.Mutex
	rows map[string]onboarding
}

func newStore() *store {
	return &store{rows: map[string]onboarding{
		"onb_4f9x": {ID: "onb_4f9x", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Owner: "owner@example.com", Stage: "draft"},
	}}
}

func (s *store) list() []onboarding {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]onboarding, 0, len(s.rows))
	for _, item := range s.rows {
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b onboarding) int { return strings.Compare(a.ID, b.ID) })
	return items
}

func (s *store) get(id string) (onboarding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.rows[id]
	return item, ok
}

func (s *store) upsert(id string, in upsertRequest) (onboarding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.rows[id]
	if !exists {
		item = onboarding{ID: id, CreatedAt: time.Now().UTC()}
	}
	item.Owner, item.Stage = in.Owner, in.Stage
	s.rows[id] = item
	return item, !exists
}

func (s *store) delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return false
	}
	delete(s.rows, id)
	return true
}

func operation[Req, Res any](summary string, fn http.HandlerFunc, responses ...specout.Response) specout.Handler[Req, Res] {
	return specout.Handler[Req, Res]{HandlerFunc: fn, Summary: summary, Tags: []string{"onboarding"}, Responses: responses}
}

func invalid(w http.ResponseWriter, field, message string) {
	examplekit.WriteJSON(w, http.StatusUnprocessableEntity, validationError{[]fieldProblem{{Field: field, Message: message}}})
}

func upsert(s *store, id func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in upsertRequest
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if !strings.Contains(in.Owner, "@") {
			invalid(w, "owner", "must be a valid email")
			return
		}
		item, created := s.upsert(id(r), in)
		if created {
			w.Header().Set("Location", "/onboarding/"+item.ID)
			examplekit.WriteJSON(w, http.StatusCreated, item)
			return
		}
		examplekit.WriteJSON(w, http.StatusOK, item)
	}
}

func routes(b *specout.ChiRouter, s *store) {
	b.Get("/onboarding", operation[listRequest, page]("List onboarding records", func(w http.ResponseWriter, _ *http.Request) {
		examplekit.WriteJSON(w, http.StatusOK, page{Items: s.list()})
	}).WithPublic())
	b.Get("/onboarding/{id}", operation[struct{}, onboarding]("Fetch one onboarding record", func(w http.ResponseWriter, r *http.Request) {
		item, ok := s.get(chi.URLParam(r, "id"))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		examplekit.WriteJSON(w, http.StatusOK, item)
	}).WithPublic())
	b.Post("/onboarding", operation[upsertRequest, onboarding]("Create an onboarding record", upsert(s, func(r *http.Request) string {
		if r.URL.Query().Has("existing") {
			return "onb_4f9x"
		}
		return "onb_new"
	}), specout.Response{Status: http.StatusCreated, Headers: []specout.Header{{Name: "Location"}}}, specout.Response{Status: http.StatusUnprocessableEntity, Type: validationError{}}))
	b.Put("/onboarding/{id}", operation[upsertRequest, onboarding]("Create or replace an onboarding record", upsert(s, func(r *http.Request) string {
		return chi.URLParam(r, "id")
	}), specout.Response{Status: http.StatusCreated}, specout.Response{Status: http.StatusUnprocessableEntity, Type: validationError{}}))
	b.Delete("/onboarding/{id}", operation[struct{}, specout.NoContent]("Delete an onboarding record", func(w http.ResponseWriter, r *http.Request) {
		if !s.delete(chi.URLParam(r, "id")) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	b.Post("/webhooks", operation[webhook, specout.NoContent]("Register a webhook", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}
