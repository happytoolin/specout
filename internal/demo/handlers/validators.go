package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/validators"
)

// HandleCreateThing validates nothing itself — the schema documents the
// contract; the handler stays raw std. A real app would enforce it here.
func HandleCreateThing() specout.Handler[validators.CreateThingRequest, validators.Thing] {
	return specout.Handler[validators.CreateThingRequest, validators.Thing]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var req validators.CreateThingRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				api.JSON(w, http.StatusBadRequest, api.Problem{Type: "invalid-json", Title: "Invalid JSON", Detail: err.Error()})
				return
			}
			if !slugRe.MatchString(req.Slug) {
				api.JSON(w, http.StatusUnprocessableEntity, api.ValidationError{Problems: []api.FieldProblem{
					{Field: "slug", Message: "must match ^[a-z][a-z0-9-]*$"},
				}})
				return
			}
			thing := validators.Thing{
				ID:         "thg_" + req.Slug,
				CreatedAt:  time.Now().UTC(),
				Slug:       req.Slug,
				Email:      req.Email,
				Priority:   req.Priority,
				Tags:       req.Tags,
				Visibility: req.Visibility,
				ExpiresAt:  req.ExpiresAt,
				Metadata:   req.Metadata,
			}
			api.JSON(w, http.StatusCreated, thing)
		},
		Summary: "Create a thing (all constraint keywords)",
		Tags:    []string{"validators"},
		Responses: []specout.Response{
			{Status: 200, Omit: true},
			{Status: 201},
			{Status: 422, Type: api.ValidationError{}},
		},
	}
}

// HandleSearchThings shows query-param constraints in Scalar's parameter view.
func HandleSearchThings() specout.Handler[validators.SearchThingsRequest, validators.ThingList] {
	return specout.Handler[validators.SearchThingsRequest, validators.ThingList]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			limit := 20
			if v := q.Get("limit"); v != "" {
				json.Unmarshal([]byte(v), &limit)
			}
			items := sampleThings()
			if len(items) > limit {
				items = items[:limit]
			}
			if s := q.Get("q"); s != "" {
				kept := items[:0]
				for _, t := range items {
					if strings.Contains(t.Slug, s) {
						kept = append(kept, t)
					}
				}
				items = kept
			}
			api.JSON(w, http.StatusOK, validators.ThingList{Items: items})
		},
		Summary: "Search things (query-param constraints)",
		Tags:    []string{"validators"},
	}
}

// HandleConfigureChannel shows oneOf + discriminator.
func HandleConfigureChannel() specout.Handler[validators.ChannelConfig, specout.NoContent] {
	return specout.Handler[validators.ChannelConfig, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var cfg validators.ChannelConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				api.JSON(w, http.StatusBadRequest, api.Problem{Type: "invalid-json", Title: "Invalid JSON"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		},
		Summary: "Configure a notification channel (oneOf + discriminator)",
		Tags:    []string{"validators"},
	}
}

func sampleThings() []validators.Thing {
	return []validators.Thing{
		{ID: "thg_alpha", Slug: "alpha", Email: "ops@example.com", Priority: 3, Tags: []string{"core"}, Visibility: "public"},
		{ID: "thg_beta", Slug: "beta", Email: "dev@example.com", Priority: 5, Tags: []string{"edge", "fast"}, Visibility: "internal"},
	}
}

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
