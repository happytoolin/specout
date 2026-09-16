package specout_test

import (
	"bytes"
	"encoding/json"
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
			"implicit": {AuthorizationURL: "https://x.example/auth", Scopes: map[string]string{"read": "read"}},
		}), specout.APIKey("api_key", "X-Key", "header")},
	}
}

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

// probeAssert is the whole former-gap matrix against one adapter's document.
// probeAssert is the whole former-gap matrix against one adapter's document.
func probeAssert(t *testing.T, doc map[string]any) {
	t.Helper()
	paths := doc["paths"].(map[string]any)
	for _, p := range []string{"/things/{id}", "/things"} {
		if _, ok := paths[p]; !ok {
			t.Fatalf("missing path %s", p)
		}
	}

	op := paths["/things/{id}"].(map[string]any)["get"].(map[string]any)
	got := map[string]map[string]any{}
	for _, p := range op["parameters"].([]any) {
		m := p.(map[string]any)
		got[m["name"].(string)] = m
	}
	if got["filter"]["style"] != "deepObject" || got["filter"]["explode"] != true {
		t.Errorf("filter param = %v", got["filter"])
	}
	if got["X-Req"]["in"] != "header" {
		t.Errorf("header param = %v", got["X-Req"])
	}
	if got["id"]["in"] != "path" || got["id"]["required"] != true {
		t.Errorf("path param = %v", got["id"])
	}
	resp := op["responses"].(map[string]any)
	if _, ok := resp["4XX"]; !ok {
		t.Errorf("responses = %v, want 4XX", resp)
	}
	if op["externalDocs"].(map[string]any)["description"] != "how it works" {
		t.Errorf("op externalDocs = %v", op["externalDocs"])
	}
	if op["x-rate-limit"] != float64(100) {
		t.Errorf("x-rate-limit = %v", op["x-rate-limit"])
	}

	postOp := paths["/things"].(map[string]any)["post"].(map[string]any)
	reqContent := postOp["requestBody"].(map[string]any)["content"].(map[string]any)
	if _, ok := reqContent["application/xml"]; !ok {
		t.Errorf("request content = %v", reqContent)
	}
	resContent := postOp["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if len(resContent) != 2 {
		t.Errorf("response content = %v", resContent)
	}

	info := doc["info"].(map[string]any)
	if info["termsOfService"] != "https://x.example/tos" || info["contact"].(map[string]any)["email"] != "api@x.example" || info["license"].(map[string]any)["name"] != "MIT" {
		t.Errorf("info = %v", info)
	}
	if doc["tags"].([]any)[0].(map[string]any)["externalDocs"].(map[string]any)["url"] != "https://x.example/things" {
		t.Errorf("tag externalDocs = %v", doc["tags"])
	}
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	flows := schemes["oauth"].(map[string]any)["flows"].(map[string]any)["implicit"].(map[string]any)
	if flows["authorizationUrl"] != "https://x.example/auth" {
		t.Errorf("oauth flows = %v", flows)
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
// emits (catch-alls are the only allowed omission).
func probeInvariant(t *testing.T, d *specout.Generator, doc map[string]any) {
	t.Helper()
	st, err := d.SpecStatuses()
	if err != nil {
		t.Fatal(err)
	}
	declared, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
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

func probeBuild(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func probeServe(rec *recorder.Recorder, targets ...string) {
	for _, target := range targets {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/things", strings.NewReader("{}")))
}

// TestGapFeaturesOnEveryAdapter: the gap features are registration and
// rendering, so the document must not depend on which adapter found the
// route. Each adapter registers the same handlers, gets the same document,
// and passes the same drift check.
func TestGapFeaturesOnEveryAdapter(t *testing.T) {
	t.Run("chi", func(t *testing.T) {
		d := specout.New(probeCfg())
		r := chi.NewRouter()
		b := specout.Chi(d, r)
		probeRegister(b.Get[probeQ, probeRes], b.Post[probeBody, probeRes])
		if err := b.Adopt(); err != nil {
			t.Fatal(err)
		}
		probeCheck(t, d, r, probeBuild(t, d))
	})

	t.Run("gorilla", func(t *testing.T) {
		d := specout.New(probeCfg())
		r := mux.NewRouter()
		b := specout.Gorilla(d, r)
		probeRegister(b.Get[probeQ, probeRes], b.Post[probeBody, probeRes])
		if err := b.Adopt(); err != nil {
			t.Fatal(err)
		}
		probeCheck(t, d, r, probeBuild(t, d))
	})

	t.Run("std", func(t *testing.T) {
		d := specout.New(probeCfg())
		r := http.NewServeMux()
		b := specout.Std(d, r)
		probeRegister(b.Get[probeQ, probeRes], b.Post[probeBody, probeRes])
		probeCheck(t, d, r, probeBuild(t, d))
	})
}

// probeCheck asserts the document, the SpecStatuses invariant, and that the
// recorder sees no drift once the documented codes are produced.
func probeCheck(t *testing.T, d *specout.Generator, r http.Handler, doc map[string]any) {
	t.Helper()
	probeAssert(t, doc)
	probeInvariant(t, d, doc)
	rec := recorder.New(r)
	probeServe(rec, "/things/1", "/things/1?fail=1")
	recorder.Verify(t, d, rec)
}
