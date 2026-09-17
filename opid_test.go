package specout_test

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

func TestOperationIDOverride(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get("/users/{id}/profile", specout.Handler[struct{}, struct{ V int }]{
		HandlerFunc: noop,
		OperationID: "getUserProfile",
	})
	specout.Chi(d, r).Get("/users/{id}/settings", specout.Handler[struct{}, struct{ V int }]{
		HandlerFunc: noop,
	})
	doc := serveDoc(t, d, r)
	get := func(p string) string {
		op := doc["paths"].(map[string]any)[p].(map[string]any)["get"].(map[string]any)
		return op["operationId"].(string)
	}
	if got := get("/users/{id}/profile"); got != "getUserProfile" {
		t.Errorf("override ignored: %s", got)
	}
	if got := get("/users/{id}/settings"); got != "getUsersIdSettings" {
		t.Errorf("derived changed: %s", got)
	}
}
