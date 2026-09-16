package specout_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

func TestOperationIDOverride(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	d.Get(r, "/users/{id}/profile", specout.Handler[struct{}, struct{ V int }]{
		HandlerFunc: func(http.ResponseWriter, *http.Request) {},
		OperationID: "getUserProfile",
	})
	d.Get(r, "/users/{id}/settings", specout.Handler[struct{}, struct{ V int }]{
		HandlerFunc: func(http.ResponseWriter, *http.Request) {},
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
