package specout_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
)

type docReq struct {
	Session string `cookie:"session"`
	Trace   string `header:"X-Trace-Id"`
	Body    string `json:"body"`
}

type docRes struct {
	OK bool `json:"ok"`
}

// TestDocFields: cookie/header params, Description, operationId, auth
// scheme, Public opt-out, ExternalDocs.
func TestDocFields(t *testing.T) {
	d := specout.New(specout.Config{
		Title:   "t",
		Version: "1",
		Auth: []specout.AuthScheme{
			specout.Bearer,
			specout.APIKey("apiKey", "X-API-Key", specout.InHeader),
		},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example"},
	})
	r := chi.NewRouter()
	specout.Chi(d, r).Post("/things", specout.Handler[docReq, docRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
		Summary:     "s",
		Description: "longer description",
	})
	specout.Chi(d, r).Get("/ping", specout.Handler[struct{}, docRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
		Public:      true,
	})
	doc := serveDoc(t, d, r)
	// cookie + header params
	post := opOf(t, doc, "/things", "post")
	params := paramsOf(t, post)
	assert.Equal(t, "cookie", params["session"]["in"], "session param")
	assert.Equal(t, "header", params["X-Trace-Id"]["in"], "X-Trace-Id param")
	assert.Equal(t, "longer description", post["description"], "description missing")
	assert.Equal(t, "postThings", post["operationId"], "operationId")
	assert.NotContains(t, post, "security", "non-public route inherits top-level security")

	// schemes + top-level security alternatives (OR semantics)
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	bearer := schemes["bearerAuth"].(map[string]any)
	assert.Equal(t, "http", bearer["type"], "bearer type")
	assert.Equal(t, "bearer", bearer["scheme"], "bearer scheme")
	apikey := schemes["apiKey"].(map[string]any)
	assert.Equal(t, "apiKey", apikey["type"], "apiKey type")
	assert.Equal(t, "X-API-Key", apikey["name"], "apiKey name")
	assert.Equal(t, "header", apikey["in"], "apiKey in")
	sec := doc["security"].([]any)
	assert.Len(t, sec, 2, "security alternatives")

	// public route: security: []
	ping := opOf(t, doc, "/ping", "get")
	assert.Empty(t, ping["security"], "public route security")

	// externalDocs
	assert.Equal(t, "https://x.example", doc["externalDocs"].(map[string]any)["url"], "externalDocs")
}
