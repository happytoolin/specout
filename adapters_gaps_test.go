package specout_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type probeQ struct {
	Filter map[string]string `jsonschema:"style=deepObject,explode=true" query:"filter"`
	Hdr    string            `header:"X-Req"`
}

type probeBody struct {
	Old string `json:"old"     jsonschema:"deprecated"`
	Tag string `json:"tagged"  jsonschema_extras:"x-ms-identifiers=id"`
	Ex  string `json:"example" jsonschema:"example=a,example=b"`
}

type probeRes struct {
	OK bool `json:"ok"`
}

func probeCfg() specout.Config {
	return specout.Config{
		Title: "probe", Version: "1", TermsOfService: "https://x.example/tos",
		Contact: &specout.Contact{Name: "API team", Email: "api@x.example"},
		License: &specout.License{Name: "MIT", URL: "https://x.example/mit"},
		Tags:    []specout.Tag{{Name: "things", ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/things"}}},
		Auth: []specout.AuthScheme{specout.OAuth2("oauth", map[string]specout.OAuth2Flow{
			"implicit": {
				AuthorizationURL: "https://x.example/auth",
				Scopes:           map[string]string{"write:pets": "modify pets", "read:pets": "read pets"},
			},
		}), specout.APIKey("api_key", "X-Key", "header")},
	}
}

// probeRegister puts the fixture routes on one adapter's verb binders.
func probeRegister(get func(string, specout.Handler[probeQ, probeRes]), post func(string, specout.Handler[probeBody, probeRes])) {
	get("/things/{id}", specout.Handler[probeQ, probeRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("fail") != "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
		Responses:    []specout.Response{{Key: "4XX", Type: probeBody{}}},
		Tags:         []string{"things"},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/op", Description: "how it works"},
		Raw:          map[string]any{"x-rate-limit": 100},
	})
	post("/things", specout.Handler[probeBody, probeRes]{
		HandlerFunc:         func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		RequestContentTypes: []string{"application/json", "application/xml"},
		Responses:           []specout.Response{{Status: 200, ContentTypes: []string{"application/json", "application/xml"}}},
	})
}

// probeAssert is the whole gap matrix against one adapter's document. The
// features are registration and rendering, so the document must not depend on
// which adapter found the route: this one assertion set runs on all three.
func probeAssert(t *testing.T, doc map[string]any) {
	t.Helper()
	op := opOf(t, doc, "/things/{id}", "get")
	params := paramsOf(t, op)
	assert.Equal(t, "deepObject", params["filter"]["style"], "filter param")
	assert.Equal(t, true, params["filter"]["explode"], "filter param")
	// a parameter keyword, not a schema keyword: the schema must not grow one
	sch := params["filter"]["schema"].(map[string]any)
	assert.Nil(t, sch["style"], "style/explode belong to the parameter: %v", sch)
	assert.Nil(t, sch["explode"], "style/explode belong to the parameter: %v", sch)
	assert.Equal(t, "header", params["X-Req"]["in"], "header param")
	assert.Equal(t, "path", params["id"]["in"], "path param")
	assert.Equal(t, true, params["id"]["required"], "path param")

	resp := op["responses"].(map[string]any)
	assert.Contains(t, resp, "4XX", "responses")
	assert.NotContains(t, resp, "404", "a range key must not fan out into one code")
	assert.Contains(t, resp, "200", "the Res default 200 stays beside an explicit response")
	assert.Equal(t, "how it works", op["externalDocs"].(map[string]any)["description"], "op externalDocs")
	assert.InEpsilon(t, 100, op["x-rate-limit"], 0.0001, "x-rate-limit")

	postOp := opOf(t, doc, "/things", "post")
	assert.Contains(t, postOp["requestBody"].(map[string]any)["content"].(map[string]any), "application/xml", "request content")
	resContent := postOp["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	require.Len(t, resContent, 2, "response content = %v, want two media types", resContent)
	jsonRef := resContent["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	xmlRef := resContent["application/xml"].(map[string]any)["schema"].(map[string]any)["$ref"]
	assert.Equal(t, "#/components/schemas/probeRes", jsonRef, "media type schemas = %v and %v", jsonRef, xmlRef)
	assert.Equal(t, jsonRef, xmlRef, "media type schemas = %v and %v", jsonRef, xmlRef)

	info := doc["info"].(map[string]any)
	assert.Equal(t, "https://x.example/tos", info["termsOfService"], "info")
	assert.Equal(t, "MIT", info["license"].(map[string]any)["name"], "info")
	contact := info["contact"].(map[string]any)
	assert.Equal(t, "api@x.example", contact["email"], "contact")
	assert.NotContains(t, contact, "url", "an empty contact field must not be emitted")
	assert.Equal(t, "https://x.example/things",
		doc["tags"].([]any)[0].(map[string]any)["externalDocs"].(map[string]any)["url"], "tag externalDocs")

	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	implicit := schemes["oauth"].(map[string]any)["flows"].(map[string]any)["implicit"].(map[string]any)
	assert.Equal(t, "https://x.example/auth", implicit["authorizationUrl"], "oauth flows")
	scopes := implicit["scopes"].(map[string]any)
	assert.Len(t, scopes, 2, "scopes")
	assert.Equal(t, "modify pets", scopes["write:pets"], "scopes")
	assert.Equal(t, "X-Key", schemes["api_key"].(map[string]any)["name"], "api_key")

	bProps := schemas(t, doc)["probeBody"].(map[string]any)["properties"].(map[string]any)
	assert.Equal(t, true, bProps["old"].(map[string]any)["deprecated"], "probeBody props")
	assert.Equal(t, "id", bProps["tagged"].(map[string]any)["x-ms-identifiers"], "probeBody props")
	ex, _ := bProps["example"].(map[string]any)["examples"].([]any)
	assert.Len(t, ex, 2, "examples")
}

// probeInvariant: every path SpecStatuses knows about is a path the document
// emits (catch-alls are the only allowed omission), and a bare range key allows
// a 404 without requiring one.
func probeInvariant(t *testing.T, d *specout.Generator, doc map[string]any) {
	t.Helper()
	st, declared := specStatuses(t, d), declaredStatuses(t, d)
	paths := doc["paths"].(map[string]any)
	for k := range st {
		assert.Contains(t, paths, k.Path, "SpecStatuses path %s is not in the document", k.Path)
	}
	key := specout.RouteKey{Method: "GET", Path: "/things/{id}"}
	assert.True(t, st[key][404], "4XX must allow 404")
	assert.False(t, declared[key][404], "a bare range must not require 404")
}

// probeServe produces the fixture's documented codes through the recorder.
func probeServe(rec *recorder.Recorder, targets ...string) {
	for _, target := range targets {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
	}
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/things", strings.NewReader("{}")))
}

// probeCheck asserts the document and the drift view, then requires a clean
// drift check once the documented codes are produced.
func probeCheck(t *testing.T, d *specout.Generator, r http.Handler, doc map[string]any) {
	t.Helper()
	probeAssert(t, doc)
	probeInvariant(t, d, doc)
	rec := recorder.New(r)
	probeServe(rec, "/things/1", "/things/1?fail=1")
	recorder.Verify(t, d, rec)
}

// TestGapFeaturesOnEveryAdapter registers one fixture on each adapter and
// requires the same document, the same drift view and the same clean check.
func TestGapFeaturesOnEveryAdapter(t *testing.T) {
	t.Run("chi", func(t *testing.T) {
		d, r := specout.New(probeCfg()), chi.NewRouter()
		b := specout.Chi(d, r)
		probeRegister(b.Get[probeQ, probeRes], b.Post[probeBody, probeRes])
		adopt(t, b)
		probeCheck(t, d, r, buildDoc(t, d))
	})
	t.Run("gorilla", func(t *testing.T) {
		d, r := specout.New(probeCfg()), mux.NewRouter()
		b := specout.Gorilla(d, r)
		probeRegister(b.Get[probeQ, probeRes], b.Post[probeBody, probeRes])
		adopt(t, b)
		probeCheck(t, d, r, buildDoc(t, d))
	})
	t.Run("std", func(t *testing.T) {
		d, r := specout.New(probeCfg()), http.NewServeMux()
		b := specout.Std(d, r)
		probeRegister(b.Get[probeQ, probeRes], b.Post[probeBody, probeRes])
		probeCheck(t, d, r, buildDoc(t, d))
	})
}
