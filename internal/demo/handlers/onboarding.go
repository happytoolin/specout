package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/onboarding"
)

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
	return specout.Get[onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) { get(w, d, chi.URLParam(r, "id")) },
		Summary:     "Fetch one onboarding record",
		Tags:        []string{"onboarding"},
		Public:      true,
	}
}

// HandleLegacyGet serves the deprecated /legacy endpoint; the closure keeps router-walk on its own path.
func HandleLegacyGet(d Deps) specout.Handler[struct{}, onboarding.Onboarding] {
	return specout.Handler[struct{}, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			get(w, d, "onb_4f9x")
		},
		Summary: "Replaced by GET /onboarding/{id}",
		Tags:    []string{"onboarding"},
		Public:  true,
	}.WithDeprecated()
}

func HandleUpsert(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) { upsert(w, d, r, chi.URLParam(r, "id"), true) },
	}.
		WithSummary("Create or replace an onboarding record").
		WithTags("onboarding").
		WithResponses(
			specout.Response{Status: http.StatusCreated},
			specout.Response{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}},
		)
}

func HandleDelete(d Deps) specout.Handler[struct{}, specout.NoContent] {
	return specout.Delete{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if err := d.Store.Delete(chi.URLParam(r, "id")); err != nil {
				writeErr(w, d, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		},
		Summary: "Delete an onboarding record",
		Tags:    []string{"onboarding"},
	}
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
