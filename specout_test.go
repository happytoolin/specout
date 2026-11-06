package specout_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type Onboarding struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Stage string `json:"stage"`
}

type UpsertRequest struct {
	Owner string `json:"owner"`
	Stage string `json:"stage"`
}

type EmptyReq struct{}

func TestQuickStartFlow(t *testing.T) {
	d, r := specout.New(specout.Config{
		Title:         "Onboarding API",
		Version:       "1.0.0",
		Description:   "Internal onboarding service.",
		ErrorType:     Problem{},
		DefaultErrors: []int{400, 404, 500},
	}), http.NewServeMux()

	rc := specout.Bind(d, specoutstd.New(r))
	rc.Handle("DELETE", "/onboarding/{id}", specout.Handler[EmptyReq, struct{}]{HandlerFunc: okBody})
	rc.Handle("PUT", "/onboarding/{id}", specout.Handler[UpsertRequest, Onboarding]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusCreated}},
	})
	rc.Handle("GET", "/onboarding", specout.Handler[EmptyReq, []Onboarding]{HandlerFunc: noop, Summary: "List onboarding"})
	fetch := func() []byte {
		spec, err := d.Build()
		require.NoError(t, err)
		return spec
	}
	body := fetch()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(body, &doc))

	require.Equal(t, "3.1.0", doc["openapi"])
	paths := pathsObj(doc)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		assert.Contains(t, paths, p, "missing path %q", p)
	}

	assert.Contains(t, opOf(t, doc, "/onboarding/{id}", "delete")["responses"].(map[string]any), "204", "delete missing 204")
	putResps := opOf(t, doc, "/onboarding/{id}", "put")["responses"].(map[string]any)
	assert.Contains(t, putResps, "201", "put missing 201")
	assert.NotContains(t, putResps, "200", "the declared 201 replaces the 200")
	for _, code := range []string{"400", "404", "500"} {
		assert.Contains(t, putResps, code, "put missing global default")
	}

	assert.Contains(t, schemas(t, doc), "Onboarding", "missing Onboarding component")
	ref := func(code string) any {
		return putResps[code].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	}
	assert.Equal(t, "#/components/schemas/Onboarding", ref("201"), "the 201 body")
	assert.Equal(t, "#/components/schemas/Problem", ref("404"), "the 404 body")

	assert.True(t, bytes.Equal(body, fetch()), "spec not deterministic across builds")

	require.Panics(t, func() { rc.Handle("GET", "/late", specout.Handler[EmptyReq, Onboarding]{HandlerFunc: noop}) })
}

func TestStdMuxHandle(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(mux)).Handle("DELETE", "/onboarding/{id}", specout.Handler[EmptyReq, struct{}]{HandlerFunc: okBody})
	specout.Bind(d, specoutstd.New(mux)).Handle("GET", "/onboarding", specout.Handler[EmptyReq, []Onboarding]{HandlerFunc: noop})

	doc := buildDoc(t, d)
	for _, p := range []string{"/onboarding", "/onboarding/{id}"} {
		assert.Contains(t, pathsObj(doc), p, "missing std path %q", p)
	}
	assert.Contains(t, opOf(t, doc, "/onboarding/{id}", "delete")["responses"].(map[string]any), "204", "std delete missing 204")
}
