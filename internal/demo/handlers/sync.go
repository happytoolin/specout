package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/specoutapi"
)

// HandleSync shows the DetailedError flow: the store returns *ConflictError,
// the handler keeps ONE error exit, and the 409 body is SyncConflict — not
// the global Problem. Declared in Responses, produced by Payload().
func HandleSync(d Deps) specout.Handler[onboarding.SyncRequest, onboarding.SyncResult] {
	return specout.Handler[onboarding.SyncRequest, onboarding.SyncResult]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			req, err := specoutapi.Decode[onboarding.SyncRequest](r)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			if req.Expected < 0 {
				d.API.Status(w, http.StatusUnprocessableEntity, api.ValidationError{
					Problems: []api.FieldProblem{{Field: "expected", Message: "must be zero or positive"}},
				})
				return
			}
			result, err := d.Store.Sync(chi.URLParam(r, "id"), req.Expected)
			if err != nil {
				d.API.Err(w, r, err) // ConflictError carries its own 409 + shape
				return
			}
			d.API.OK(w, result)
		},
		Summary: "Sync a record against an expected version",
		Tags:    []string{"sync"},
		Responses: []specout.Response{
			{Status: http.StatusConflict, Type: onboarding.SyncConflict{}},
			{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}},
			{Status: http.StatusUnauthorized, Omit: true}, // public route — 401 impossible
		},
	}
}
