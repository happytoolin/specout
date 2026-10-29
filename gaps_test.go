package specout_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
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
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
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

// TestGapNoBodyStatuses: a bare 1xx, 204, 205 or 304 carries no body. HTTP
// forbids content there, and inheriting Res would leak the handler's schema
// into it.
func TestGapNoBodyStatuses(t *testing.T) {
	d := newGen()
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/gone", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		Responses:   []specout.Response{{Status: 101}, {Status: 204}, {Status: 205}, {Status: 304}, {Key: "1XX"}},
	})
	responses := opOf(t, serveDoc(t, d, r), "/gone", "get")["responses"].(map[string]any)
	for _, code := range []string{"101", "204", "205", "304", "1XX"} {
		resp, ok := responses[code].(map[string]any)
		require.True(t, ok, "missing %s", code)
		assert.NotContains(t, resp, "content", "%s must carry no content", code)
		assert.NotEmpty(t, resp["description"], "%s needs a description", code)
	}
	assert.Contains(t, responses, "200", "the Res default 200 stays")
}

// TestGapHeadCarriesNoContent: RFC 9110 gives a HEAD response no body, so a
// HEAD operation keeps each status, description and header, and no content.
func TestGapHeadCarriesNoContent(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1", ErrorType: gapRes{}, DefaultErrors: []int{http.StatusInternalServerError},
	})
	specout.Document(d, http.MethodHead, "/x", specout.Handler[struct{}, gapRes]{
		HandlerFunc: noop,
		Responses: []specout.Response{
			{Status: http.StatusNotFound, Headers: []specout.Header{{Name: "X-Reason"}}},
			{Status: http.StatusPartialContent, ContentType: "application/pdf"},
		},
	})
	doc := buildDoc(t, d)
	responses, _ := opOf(t, doc, "/x", "head")["responses"].(map[string]any)
	require.Len(t, responses, 4)
	for code, want := range map[string]string{"200": "OK", "404": "Not Found", "206": "Partial Content", "500": "Internal Server Error"} {
		resp, _ := responses[code].(map[string]any)
		assert.NotContains(t, resp, "content", code)
		assert.Equal(t, want, resp["description"], code)
	}
	assert.Contains(t, responses["404"], "headers")
	assert.NotContains(t, doc, "components", "a HEAD operation reflects no body type")
}

// TestGapBodylessContentPanics: 1xx, 204, 205 and 304 carry no body, so a
// Response there with a Type that has fields or a media type panics at
// registration. An empty Type and a range key that names 204 stay legal.
func TestGapBodylessContentPanics(t *testing.T) {
	for key, resp := range map[string]specout.Response{
		"204": {Status: http.StatusNoContent, Type: gapRes{}},
		"205": {Status: http.StatusResetContent, Type: gapRes{}},
		"304": {Status: http.StatusNotModified, ContentType: "text/plain"},
		"1XX": {Key: "1XX", ContentTypes: []string{"application/json"}},
	} {
		h := specout.Handler[struct{}, gapRes]{HandlerFunc: noop, Responses: []specout.Response{resp}}
		assert.PanicsWithValue(t,
			"specout: response "+key+" carries no body; remove its Type, ContentType or ContentTypes",
			func() { specout.Document(newGen(), http.MethodGet, "/x", h) }, key)
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Handler[struct{}, gapRes]{
		HandlerFunc: noop,
		Responses: []specout.Response{
			{Status: http.StatusNoContent, Type: struct{}{}},
			{Key: "2XX", Status: http.StatusNoContent},
		},
	})
	responses, _ := opOf(t, buildDoc(t, d), "/x", "get")["responses"].(map[string]any)
	assert.NotContains(t, responses["204"], "content")
	assert.Contains(t, responses["2XX"], "content", "a 2XX response describes bodies too")
}

// TestGapRequestContentTypesNeedABody: media types for a request that has no
// body describe nothing, so the registration panics.
func TestGapRequestContentTypesNeedABody(t *testing.T) {
	type onlyParams struct {
		ID string `path:"id"`
	}
	assert.PanicsWithValue(t, "specout: POST /x sets RequestContentTypes, but Req has no request body", func() {
		specout.Document(newGen(), http.MethodPost, "/x", specout.Handler[struct{}, gapRes]{
			HandlerFunc: noop, RequestContentTypes: []string{"application/json"},
		})
	})
	assert.PanicsWithValue(t, "specout: POST /x/{id} sets RequestContentTypes, but Req has no request body", func() {
		specout.Document(newGen(), http.MethodPost, "/x/{id}", specout.Handler[onlyParams, gapRes]{
			HandlerFunc: noop, RequestContentTypes: []string{"application/json"},
		})
	})
}

// TestGapOAuth2: an oauth2 flow with no URL is a spec the validator rejects,
// so New panics.
func TestGapOAuth2(t *testing.T) {
	require.Panics(t, func() {
		specout.New(specout.Config{
			Title: "t", Version: "1",
			Auth: []specout.AuthScheme{specout.OAuth2("x", map[string]specout.OAuth2Flow{"implicit": {}})},
		})
	})
}

// TestGapExternalDocsNeedsURL: url is required by OpenAPI, and an empty one
// used to emit an object the validator rejects.
func TestGapExternalDocsNeedsURL(t *testing.T) {
	require.Panics(t, func() {
		specout.New(specout.Config{
			Title: "t", Version: "1",
			ExternalDocs: &specout.ExternalDocs{Description: "no link"},
		})
	})
}

// TestGapConfigThatBreaksTheDocument: each config emitted a document the
// OpenAPI validator rejects, or dropped a declared scheme without a word. New
// panics on it, before any route is registered.
func TestGapConfigThatBreaksTheDocument(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		cfg        specout.Config
	}{
		{"license without name", "specout: License needs a Name", specout.Config{License: &specout.License{URL: "https://x.example"}}},
		{"tag without a docs URL", "specout: ExternalDocs needs a URL", specout.Config{Tags: []specout.Tag{{Name: "a", ExternalDocs: &specout.ExternalDocs{}}}}},
		{
			"reflect.Type error envelope", "specout: a Type field takes a value such as Error{}, not a reflect.Type",
			specout.Config{ErrorType: reflect.TypeFor[gapRes](), DefaultErrors: []int{http.StatusInternalServerError}},
		},
		{
			"error status out of range", "specout: response status must be 0 (default) or 100-599, got 600",
			specout.Config{ErrorType: gapRes{}, DefaultErrors: []int{600}},
		},
		{
			"error status with no body", "specout: DefaultErrors holds 204, which carries no body for ErrorType",
			specout.Config{ErrorType: gapRes{}, DefaultErrors: []int{http.StatusNoContent}},
		},
		{"tag without name", `specout: empty or duplicate tag name ""`, specout.Config{Tags: []specout.Tag{{Description: "d"}}}},
		{"duplicate tag", `specout: empty or duplicate tag name "a"`, specout.Config{Tags: []specout.Tag{{Name: "a"}, {Name: "a"}}}},
		{
			"auth name with a space", `specout: AuthScheme.Name "bearer auth" must contain only ASCII letters, digits, dots, hyphens or underscores`,
			specout.Config{Auth: []specout.AuthScheme{{Name: "bearer auth", Type: "httpBearer"}}},
		},
		{
			"duplicate auth name", "specout: duplicate AuthScheme name bearerAuth",
			specout.Config{Auth: []specout.AuthScheme{specout.Bearer, specout.APIKey("bearerAuth", "k", specout.InHeader)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Title, tc.cfg.Version = "t", "1"
			assert.PanicsWithValue(t, tc.want, func() { specout.New(tc.cfg) })
		})
	}
}

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
