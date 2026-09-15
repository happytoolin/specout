package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/onboarding"
)

// HandleSync shows the typed-error flow: the store returns *ConflictError,
// the handler keeps ONE error exit, and the 409 body is SyncConflict — not
// the global Problem. Declared in Responses, produced by Payload().
func HandleSync(d Deps) specout.Handler[onboarding.SyncRequest, onboarding.SyncResult] {
	return specout.Handler[onboarding.SyncRequest, onboarding.SyncResult]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var req onboarding.SyncRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, d, err)
				return
			}
			if req.Expected < 0 {
				api.JSON(w, http.StatusUnprocessableEntity, api.ValidationError{
					Problems: []api.FieldProblem{{Field: "expected", Message: "must be zero or positive"}},
				})
				return
			}
			result, err := d.Store.Sync(chi.URLParam(r, "id"), req.Expected)
			if err != nil {
				writeErr(w, d, err) // ConflictError carries its own 409 + shape
				return
			}
			api.JSON(w, http.StatusOK, result)
		},
		Summary: "Sync a record against an expected version",
		Description: "Optimistic concurrency: the client sends the version it last saw; a mismatch returns 409 with the current version and a resolve URL.",
		Tags:    []string{"sync"},
		Responses: []specout.Response{
			{Status: http.StatusConflict, Type: onboarding.SyncConflict{}},
			{Status: http.StatusUnprocessableEntity, Type: api.ValidationError{}},
			{Status: http.StatusUnauthorized, Omit: true}, // public route — 401 impossible
		},
	}
}
