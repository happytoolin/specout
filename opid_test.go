package specout_test

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
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
	get := func(p string) string { return opOf(t, doc, p, "get")["operationId"].(string) }
	assert.Equal(t, "getUserProfile", get("/users/{id}/profile"), "override ignored")
	assert.Equal(t, "getUsersIdSettings", get("/users/{id}/settings"), "derived changed")
}
