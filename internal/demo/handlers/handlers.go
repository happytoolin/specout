// Package handlers holds the demo factories: types ride the type parameters, the closure stays a plain http.HandlerFunc.
package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/internal/demo/validators"
)

// Deps is what every factory receives; Mapper is the app's own error mapper.
type Deps struct {
	Store  *onboarding.Store
	Mapper api.Mapper
}

// op builds a handler in one call: shared metadata plus the closure; res is optional.
func op[Req, Res any](tag, summary string, public bool, fn http.HandlerFunc, res ...specout.Response) specout.Handler[Req, Res] {
	return specout.Handler[Req, Res]{HandlerFunc: fn, Summary: summary, Tags: []string{tag}, Public: public, Responses: res}
}

func writeErr(w http.ResponseWriter, d Deps, err error) { api.Error(w, d.Mapper, err) }

// queryInt reads an int off the raw request (query tags are not auto-bound); malformed input keeps def.
func queryInt(q url.Values, key string, def int) int {
	if v := q.Get(key); v != "" {
		json.Unmarshal([]byte(v), &def)
	}
	return def
}

func HandleList(d Deps) specout.Handler[onboarding.ListRequest, onboarding.Page] {
	return op[onboarding.ListRequest, onboarding.Page]("onboarding", "List onboarding records", true, func(w http.ResponseWriter, r *http.Request) {
		items := d.Store.List()
		if n := queryInt(r.URL.Query(), "limit", 0); n > 0 && len(items) > n {
			items = items[:n]
		}
		api.JSON(w, http.StatusOK, onboarding.Page{Items: items})
	})
}

// upsert is the shared create/replace tail: 201 + Location when new, else 200.
func upsert(w http.ResponseWriter, d Deps, r *http.Request, id string, checkOwner bool) {
	var req onboarding.UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, d, err)
		return
	}
	if checkOwner && req.Owner != "" && !strings.Contains(req.Owner, "@") {
		api.Invalid(w, "owner", "must be a valid email")
		return
	}
	ob, created, err := d.Store.Upsert(id, req)
	if err != nil {
		writeErr(w, d, err)
		return
	}
	if !created {
		api.JSON(w, http.StatusOK, ob)
		return
	}
	w.Header().Set("Location", "/onboarding/"+ob.ID)
	api.JSON(w, http.StatusCreated, ob)
}

func HandleCreate(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return op[onboarding.UpsertRequest, onboarding.Onboarding]("onboarding", "Create an onboarding record", false, func(w http.ResponseWriter, r *http.Request) {
		id := "onb_n3w"
		if r.URL.Query().Get("existing") != "" {
			id = "onb_4f9x" // idempotent re-create
		}
		upsert(w, d, r, id, false)
	}, specout.Response{Status: http.StatusCreated, Headers: []specout.Header{{Name: "Location"}}})
}

func get(w http.ResponseWriter, d Deps, id string) {
	ob, err := d.Store.Get(id)
	if err != nil {
		writeErr(w, d, err)
		return
	}
	api.JSON(w, http.StatusOK, ob)
}

func HandleGet(d Deps) specout.Handler[struct{}, onboarding.Onboarding] {
	return op[struct{}, onboarding.Onboarding]("onboarding", "Fetch one onboarding record", true, func(w http.ResponseWriter, r *http.Request) {
		get(w, d, chi.URLParam(r, "id"))
	})
}

// HandleLegacyGet serves the deprecated /legacy endpoint; the closure keeps router-walk on its own path.
func HandleLegacyGet(d Deps) specout.Handler[struct{}, onboarding.Onboarding] {
	h := op[struct{}, onboarding.Onboarding]("onboarding", "Replaced by GET /onboarding/{id}", true, func(w http.ResponseWriter, _ *http.Request) {
		get(w, d, "onb_4f9x")
	})
	h.Deprecated = true
	return h
}

func HandleUpsert(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return op[onboarding.UpsertRequest, onboarding.Onboarding]("onboarding", "Create or replace an onboarding record", false, func(w http.ResponseWriter, r *http.Request) {
		upsert(w, d, r, chi.URLParam(r, "id"), true)
	}, specout.Response{Status: http.StatusCreated}, specout.Response{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}})
}

func HandleDelete(d Deps) specout.Handler[struct{}, specout.NoContent] {
	return op[struct{}, specout.NoContent]("onboarding", "Delete an onboarding record", false, func(w http.ResponseWriter, r *http.Request) {
		if err := d.Store.Delete(chi.URLParam(r, "id")); err != nil {
			writeErr(w, d, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// HandleSync shows the typed-error flow: *ConflictError keeps one error exit and its own 409 body.
func HandleSync(d Deps) specout.Handler[onboarding.SyncRequest, onboarding.SyncResult] {
	h := op[onboarding.SyncRequest, onboarding.SyncResult]("sync", "Sync a record against an expected version", false, func(w http.ResponseWriter, r *http.Request) {
		var req onboarding.SyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, d, err)
			return
		}
		if req.Expected < 0 {
			api.Invalid(w, "expected", "must be zero or positive")
			return
		}
		result, err := d.Store.Sync(chi.URLParam(r, "id"), req.Expected)
		if err != nil {
			writeErr(w, d, err) // ConflictError carries its own 409 + shape
			return
		}
		api.JSON(w, http.StatusOK, result)
	}, specout.Response{Status: http.StatusConflict, Type: onboarding.SyncConflict{}}, specout.Response{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}},
		specout.Response{Status: http.StatusUnauthorized, Omit: true}) // public route — 401 impossible
	h.Description = "Optimistic concurrency: the client sends the version it last saw; a mismatch returns 409 with the current version and a resolve URL."
	return h
}

// ImportRequest: the File field makes this multipart/form-data.
type ImportRequest struct {
	File specout.File `form:"file" jsonschema:"description=CSV of onboarding records"`
	Mode string       `form:"mode"  jsonschema:"enum=merge|replace,default=merge"`
}

// HandleImport: NoContent as Res means the 204 is the whole contract.
func HandleImport(d Deps) specout.Handler[ImportRequest, specout.NoContent] {
	return op[ImportRequest, specout.NoContent]("files", "Bulk import from CSV", false, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func HandleReport(d Deps) specout.Handler[struct{}, struct{}] {
	// overrides the struct{} 204 default: this route returns a PDF at 200
	return op[struct{}, struct{}]("files", "Download the activity report", true, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=report.pdf")
		w.Write([]byte("%PDF-1.4 demo report"))
	}, specout.Response{Status: http.StatusOK, ContentType: "application/pdf"})
}

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
