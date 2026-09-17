package specout_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

type probeQ struct {
	Filter map[string]string `query:"filter" jsonschema:"style=deepObject,explode=true"`
	Hdr    string            `header:"X-Req"`
}

type probeBody struct {
	Old string `json:"old" jsonschema:"deprecated"`
	Tag string `json:"tagged" jsonschema_extras:"x-ms-identifiers=id"`
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
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(200)
		},
		Responses:    []specout.Response{{Key: "4XX", Type: probeBody{}}},
		Tags:         []string{"things"},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/op", Description: "how it works"},
		Raw:          map[string]any{"x-rate-limit": 100},
	})
	post("/things", specout.Handler[probeBody, probeRes]{
		HandlerFunc:         func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
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
	params := map[string]map[string]any{}
	for _, raw := range op["parameters"].([]any) {
		p := raw.(map[string]any)
		params[p["name"].(string)] = p
	}
	if params["filter"]["style"] != "deepObject" || params["filter"]["explode"] != true {
		t.Errorf("filter param = %v", params["filter"])
	}
	// a parameter keyword, not a schema keyword: the schema must not grow one
	if sch := params["filter"]["schema"].(map[string]any); sch["style"] != nil || sch["explode"] != nil {
		t.Errorf("style/explode belong to the parameter, not its schema: %v", sch)
	}
	if params["X-Req"]["in"] != "header" {
		t.Errorf("header param = %v", params["X-Req"])
	}
	if id := params["id"]; id["in"] != "path" || id["required"] != true {
		t.Errorf("path param = %v", id)
	}
	resp := op["responses"].(map[string]any)
	if _, ok := resp["4XX"]; !ok {
		t.Errorf("responses = %v, want 4XX", resp)
	}
	if _, ok := resp["404"]; ok {
		t.Error("a range key must not fan out into one code")
	}
	if _, ok := resp["200"]; !ok {
		t.Error("the Res default 200 stays beside an explicit response")
	}
	if op["externalDocs"].(map[string]any)["description"] != "how it works" {
		t.Errorf("op externalDocs = %v", op["externalDocs"])
	}
	if op["x-rate-limit"] != float64(100) {
		t.Errorf("x-rate-limit = %v", op["x-rate-limit"])
	}

	postOp := opOf(t, doc, "/things", "post")
	reqContent := postOp["requestBody"].(map[string]any)["content"].(map[string]any)
	if _, ok := reqContent["application/xml"]; !ok {
		t.Errorf("request content = %v, want xml too", reqContent)
	}
	resContent := postOp["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if len(resContent) != 2 {
		t.Fatalf("response content = %v, want two media types", resContent)
	}
	jsonRef := resContent["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	xmlRef := resContent["application/xml"].(map[string]any)["schema"].(map[string]any)["$ref"]
	if xmlRef != jsonRef || jsonRef != "#/components/schemas/probeRes" {
		t.Errorf("media type schemas = %v and %v, want one probeRes ref", jsonRef, xmlRef)
	}

	info := doc["info"].(map[string]any)
	if info["termsOfService"] != "https://x.example/tos" || info["license"].(map[string]any)["name"] != "MIT" {
		t.Errorf("info = %v", info)
	}
	contact := info["contact"].(map[string]any)
	if contact["email"] != "api@x.example" {
		t.Errorf("contact = %v", contact)
	}
	if _, ok := contact["url"]; ok {
		t.Error("an empty contact field must not be emitted")
	}
	if doc["tags"].([]any)[0].(map[string]any)["externalDocs"].(map[string]any)["url"] != "https://x.example/things" {
		t.Errorf("tag externalDocs = %v", doc["tags"])
	}

	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	implicit := schemes["oauth"].(map[string]any)["flows"].(map[string]any)["implicit"].(map[string]any)
	if implicit["authorizationUrl"] != "https://x.example/auth" {
		t.Errorf("oauth flows = %v", implicit)
	}
	if scopes := implicit["scopes"].(map[string]any); len(scopes) != 2 || scopes["write:pets"] != "modify pets" {
		t.Errorf("scopes = %v", scopes)
	}
	if schemes["api_key"].(map[string]any)["name"] != "X-Key" {
		t.Errorf("api_key = %v", schemes["api_key"])
	}

	props := doc["components"].(map[string]any)["schemas"].(map[string]any)["probeBody"].(map[string]any)["properties"].(map[string]any)
	if props["old"].(map[string]any)["deprecated"] != true || props["tagged"].(map[string]any)["x-ms-identifiers"] != "id" {
		t.Errorf("probeBody props = %v", props)
	}
	if ex, _ := props["example"].(map[string]any)["examples"].([]any); len(ex) != 2 {
		t.Errorf("examples = %v", props["example"])
	}
}

// probeInvariant: every path SpecStatuses knows about is a path the document
// emits (catch-alls are the only allowed omission), and a bare range key allows
// a 404 without requiring one.
func probeInvariant(t *testing.T, d *specout.Generator, doc map[string]any) {
	t.Helper()
	st, declared := specStatuses(t, d), declaredStatuses(t, d)
	paths := doc["paths"].(map[string]any)
	for k := range st {
		if _, ok := paths[k.Path]; !ok {
			t.Errorf("SpecStatuses path %s is not in the document", k.Path)
		}
	}
	key := specout.RouteKey{Method: "GET", Path: "/things/{id}"}
	if !st[key][404] {
		t.Error("4XX must allow 404")
	}
	if declared[key][404] {
		t.Error("a bare range must not require 404")
	}
}

// probeServe produces the fixture's documented codes through the recorder.
func probeServe(rec *recorder.Recorder, targets ...string) {
	for _, target := range targets {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/things", strings.NewReader("{}")))
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
