package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/validators"
)

// HandleRegisterWebhook shows unions: oneOf + discriminator via d.Register[T](name).
func HandleRegisterWebhook(d Deps) specout.Handler[validators.Config, validators.Config] {
	return op[validators.Config, validators.Config]("webhooks", "Register a webhook (email or slack)", false, func(w http.ResponseWriter, r *http.Request) {
		var cfg validators.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeErr(w, d, err)
			return
		}
		api.JSON(w, http.StatusOK, cfg)
	})
}

// HandleCreateThing validates nothing itself; the schema documents the contract.
func HandleCreateThing() specout.Handler[validators.CreateThingRequest, validators.Thing] {
	return op[validators.CreateThingRequest, validators.Thing]("validators", "Create a thing (all constraint keywords)", false, func(w http.ResponseWriter, r *http.Request) {
		var req validators.CreateThingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			api.JSON(w, http.StatusBadRequest, api.Problem{Type: "invalid-json", Title: "Invalid JSON", Detail: err.Error()})
			return
		}
		if !slugRe.MatchString(req.Slug) {
			api.Invalid(w, "slug", "must match ^[a-z][a-z0-9-]*$")
			return
		}
		api.JSON(w, http.StatusCreated, req.Thing("thg_"+req.Slug, time.Now().UTC()))
	}, specout.Response{Status: 200, Omit: true}, specout.Response{Status: 201}, specout.Response{Status: 422, Type: api.ValidationError{}})
}

func HandleSearchThings() specout.Handler[validators.SearchThingsRequest, validators.ThingList] {
	return op[validators.SearchThingsRequest, validators.ThingList]("validators", "Search things (query-param constraints)", false, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		items := []validators.Thing{
			{ID: "thg_alpha", Slug: "alpha", Email: "ops@example.com", Priority: 3, Tags: []string{"core"}, Visibility: "public"},
			{ID: "thg_beta", Slug: "beta", Email: "dev@example.com", Priority: 5, Tags: []string{"edge", "fast"}, Visibility: "internal"},
		}
		if limit := queryInt(q, "limit", 20); len(items) > limit {
			items = items[:limit]
		}
		if s := q.Get("q"); s != "" {
			items = slices.DeleteFunc(items, func(t validators.Thing) bool { return !strings.Contains(t.Slug, s) })
		}
		api.JSON(w, http.StatusOK, validators.ThingList{Items: items})
	})
}

func HandleConfigureChannel() specout.Handler[validators.ChannelConfig, specout.NoContent] {
	return op[validators.ChannelConfig, specout.NoContent]("validators", "Configure a notification channel (oneOf + discriminator)", false, func(w http.ResponseWriter, r *http.Request) {
		var cfg validators.ChannelConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			api.JSON(w, http.StatusBadRequest, api.Problem{Type: "invalid-json", Title: "Invalid JSON"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

var slugRe = regexp.MustCompile("^[a-z][a-z0-9-]*$")
