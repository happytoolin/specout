package specout_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gapRes struct {
	OK bool `json:"ok"`
}

// TestGapRangeCode: naming one code beside a range makes that code a coverage
// expectation; a bare range key requires nothing.
func TestGapRangeCode(t *testing.T) {
	d := newGen()
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/partial", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(404) },
		Responses: []specout.Response{
			{Status: 404, Key: "4XX", Raw: map[string]any{"description": "not found"}},
			{Status: 200, Omit: true},
		},
	})
	doc := serveDoc(t, d, r)
	responses := opOf(t, doc, "/partial", "get")["responses"].(map[string]any)
	assert.Equal(t, "not found", responses["4XX"].(map[string]any)["description"])
	assert.NotContains(t, responses, "200", "Omit must drop the Res default")
	key := specout.RouteKey{Method: "GET", Path: "/partial"}
	declared := declaredStatuses(t, d)
	assert.True(t, declared[key][404], "the code named beside the range must be required")
	assert.False(t, declared[key][200], "Omit must drop the Res default from coverage")
}

// TestGapNoBodyStatuses: a bare 204 or 304 carries no body. HTTP forbids
// content there, and inheriting Res would leak the handler's schema into it.
func TestGapNoBodyStatuses(t *testing.T) {
	d := newGen()
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/gone", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(204) },
		Responses:   []specout.Response{{Status: 204}, {Status: 304}},
	})
	responses := opOf(t, serveDoc(t, d, r), "/gone", "get")["responses"].(map[string]any)
	for _, code := range []string{"204", "304"} {
		resp, ok := responses[code].(map[string]any)
		require.True(t, ok, "missing %s", code)
		assert.NotContains(t, resp, "content", "%s must carry no content", code)
		assert.NotEmpty(t, resp["description"], "%s needs a description", code)
	}
	assert.Contains(t, responses, "200", "the Res default 200 stays")
}

// TestGapOAuth2: an oauth2 flow with no URL is a spec the validator rejects,
// so it panics at build.
func TestGapOAuth2(t *testing.T) {
	bad := specout.New(specout.Config{Title: "t", Version: "1",
		Auth: []specout.AuthScheme{specout.OAuth2("x", map[string]specout.OAuth2Flow{"implicit": {}})}})
	require.Panics(t, func() { buildDoc(t, bad) })
}

// TestGapExternalDocsNeedsURL: url is required by OpenAPI, and an empty one
// used to emit an object the validator rejects.
func TestGapExternalDocsNeedsURL(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1",
		ExternalDocs: &specout.ExternalDocs{Description: "no link"}})
	require.Panics(t, func() { buildDoc(t, d) })
}
