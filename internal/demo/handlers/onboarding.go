// Package handlers holds one factory per endpoint. Factories take the
// app's dependencies and return specout.Handler — metadata rides the type
// parameters, the closure stays raw (w, r).
package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/specoutapi"
)

func randomID() string { return "n3w" }

// Deps is what every handler factory receives.
type Deps struct {
	Store *onboarding.Store
	API   *specoutapi.Responder
}

func HandleList(d Deps) specout.Handler[onboarding.ListRequest, onboarding.Page] {
	return specout.Handler[onboarding.ListRequest, onboarding.Page]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			req, err := specoutapi.Decode[onboarding.ListRequest](r)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			items := d.Store.List()
			if req.Limit > 0 && len(items) > req.Limit {
				items = items[:req.Limit]
			}
			d.API.OK(w, onboarding.Page{Items: items})
		},
		Summary: "List onboarding records",
		Tags:    []string{"onboarding"},
	}
}

func HandleCreate(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			req, err := specoutapi.Decode[onboarding.UpsertRequest](r)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			id := "onb_" + randomID()
			if r.URL.Query().Get("existing") != "" {
				// idempotent re-create: same body → same record, 200 not 201
				id = "onb_4f9x"
			}
			ob, created, err := d.Store.Upsert(id, req)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			if !created {
				// idempotent re-create of a known id: 200, no Location
				d.API.OK(w, ob)
				return
			}
			w.Header().Set("Location", "/onboarding/"+ob.ID)
			d.API.Status(w, http.StatusCreated, ob)
		},
		Summary: "Create an onboarding record",
		Tags:    []string{"onboarding"},
		Responses: []specout.Response{
			{Status: http.StatusCreated, Headers: []specout.Header{{Name: "Location"}}},
		},
	}
}

func HandleGet(d Deps) specout.Handler[onboarding.Empty, onboarding.Onboarding] {
	return specout.Handler[onboarding.Empty, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			ob, err := d.Store.Get(chi.URLParam(r, "id"))
			if err != nil {
				d.API.Err(w, r, err) // 404 + Problem via the mapper
				return
			}
			d.API.OK(w, ob)
		},
		Summary: "Fetch one onboarding record",
		Tags:    []string{"onboarding"},
	}
}

func HandleUpsert(d Deps) specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding] {
	return specout.Handler[onboarding.UpsertRequest, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			req, err := specoutapi.Decode[onboarding.UpsertRequest](r)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			if req.Owner != "" && !strings.Contains(req.Owner, "@") {
				d.API.Status(w, http.StatusUnprocessableEntity, api.ValidationError{
					Problems: []api.FieldProblem{{Field: "owner", Message: "must be a valid email"}},
				})
				return
			}
			ob, created, err := d.Store.Upsert(chi.URLParam(r, "id"), req)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			if created {
				w.Header().Set("Location", "/onboarding/"+ob.ID)
				d.API.Status(w, http.StatusCreated, ob)
				return
			}
			d.API.OK(w, ob)
		},
		Summary: "Create or replace an onboarding record",
		Tags:    []string{"onboarding"},
		Responses: []specout.Response{
			{Status: http.StatusCreated},
			{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}},
		},
	}
}

func HandleDelete(d Deps) specout.Handler[onboarding.Empty, specout.NoContent] {
	return specout.Handler[onboarding.Empty, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if err := d.Store.Delete(chi.URLParam(r, "id")); err != nil {
				d.API.Err(w, r, err)
				return
			}
			d.API.NoContent(w)
		},
		Summary: "Delete an onboarding record",
		Tags:    []string{"onboarding"},
	}
}

// HandleLegacyGet serves the deprecated /legacy endpoint. A distinct
// closure (not a reuse of HandleGet's) so the router walk resolves its own
// full path — shared closures are ambiguous to pointer-keyed discovery.
func HandleLegacyGet(d Deps) specout.Handler[onboarding.Empty, onboarding.Onboarding] {
	return specout.Handler[onboarding.Empty, onboarding.Onboarding]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			ob, err := d.Store.Get("onb_4f9x")
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			d.API.OK(w, ob)
		},
		Summary:    "Replaced by GET /onboarding/{id}",
		Tags:       []string{"onboarding"},
		Deprecated: true,
	}
}
