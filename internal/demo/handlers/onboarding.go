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

// Deps is what every handler factory receives. API is the app's own error
// mapper — plain data, no library involvement.
type Deps struct {
	Store  *onboarding.Store
	Mapper api.Mapper
}

func writeErr(w http.ResponseWriter, d Deps, err error) {
	api.Error(w, d.Mapper, err)
}

func HandleList(d Deps) specout.Handler[onboarding.ListRequest, onboarding.Page] {
	return specout.Handler[onboarding.ListRequest, onboarding.Page]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var req onboarding.ListRequest
			// raw std: query-tagged fields are yours to read however you like
			if v := r.URL.Query().Get("limit"); v != "" {
				json.Unmarshal([]byte(v), &req.Limit)
			}
			items := d.Store.List()
			if req.Limit > 0 && len(items) > req.Limit {
				items = items[:req.Limit]
			}
			api.JSON(w, http.StatusOK, onboarding.Page{Items: items})
		},
		Summary: "List onboarding records",
		Tags:    []string{"onboarding"},
	}
}

func HandleCreate(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var req onboarding.UpsertRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, d, err)
				return
			}
			id := "onb_n3w"
			if r.URL.Query().Get("existing") != "" {
				id = "onb_4f9x" // idempotent re-create
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
		},
		Summary: "Create an onboarding record",
		Tags:    []string{"onboarding"},
		Responses: []specout.Response{
			{Status: http.StatusOK},
			{Status: http.StatusCreated,
				Headers: []specout.Header{{Name: "Location"}}},
		},
	}
}

func HandleGet(d Deps) specout.Handler[struct{}, onboarding.Onboarding] {
	return specout.Handler[struct{}, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			ob, err := d.Store.Get(chi.URLParam(r, "id"))
			if err != nil {
				writeErr(w, d, err) // 404 + Problem via mapper
				return
			}
			api.JSON(w, http.StatusOK, ob)
		},
		Summary: "Fetch one onboarding record",
		Tags:    []string{"onboarding"},
		Public:  true, // reading one record needs no session cookie
	}
}

func HandleUpsert(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var req onboarding.UpsertRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, d, err)
				return
			}
			if req.Owner != "" && !strings.Contains(req.Owner, "@") {
				api.JSON(w, http.StatusUnprocessableEntity, api.ValidationError{
					Problems: []api.FieldProblem{{Field: "owner", Message: "must be a valid email"}},
				})
				return
			}
			ob, created, err := d.Store.Upsert(chi.URLParam(r, "id"), req)
			if err != nil {
				writeErr(w, d, err)
				return
			}
			if created {
				w.Header().Set("Location", "/onboarding/"+ob.ID)
				api.JSON(w, http.StatusCreated, ob)
				return
			}
			api.JSON(w, http.StatusOK, ob)
		},
		Summary: "Create or replace an onboarding record",
		Tags:    []string{"onboarding"},
		Responses: []specout.Response{
			{Status: http.StatusCreated},
			{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}},
		},
	}
}

func HandleDelete(d Deps) specout.Handler[struct{}, specout.NoContent] {
	return specout.Handler[struct{}, specout.NoContent]{
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

// HandleLegacyGet serves the deprecated /legacy endpoint. A distinct closure
// so router-walk discovery resolves its own full path.
func HandleLegacyGet(d Deps) specout.Handler[struct{}, onboarding.Onboarding] {
	return specout.Handler[struct{}, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			ob, err := d.Store.Get("onb_4f9x")
			if err != nil {
				writeErr(w, d, err)
				return
			}
			api.JSON(w, http.StatusOK, ob)
		},
		Summary:    "Replaced by GET /onboarding/{id}",
		Tags:       []string{"onboarding"},
		Deprecated: true,
	}
}
