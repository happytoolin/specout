package specout_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

type gapErrBody struct {
	Message string `json:"message"`
}

type gapQuery struct {
	Filter map[string]string `query:"filter" jsonschema:"style=deepObject,explode=true"`
	IDs    []int             `query:"ids" jsonschema:"style=form,explode=false"`
	Body   string            `json:"body"`
}

type gapRes struct {
	OK bool `json:"ok"`
}

type gapBody struct {
	Old     string `json:"old" jsonschema:"deprecated"`
	Tagged  string `json:"tagged" jsonschema_extras:"x-ms-identifiers=id"`
	Example string `json:"example" jsonschema:"example=a,example=b"`
}

// gapsDocOn adopts r, builds the document and returns it parsed.
func gapsDocOn(t *testing.T, d *specout.Generator, r chi.Router) map[string]any {
	t.Helper()
	if err := specout.Chi(d, r).Adopt(); err != nil {
		t.Fatal(err)
	}
	r.Mount("/openapi.json", d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	return doc
}

func gapsOp(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	item, ok := doc["paths"].(map[string]any)[path].(map[string]any)
	if !ok {
		t.Fatalf("no path %s in the document: %v", path, doc["paths"])
	}
	op, ok := item[method].(map[string]any)
	if !ok {
		t.Fatalf("no %s %s in the document", method, path)
	}
	return op
}

// TestGapRangeResponse: a 4XX key is one published entry, not a code
// fan-out, and any 4xx the handler writes is inside the spec.
func TestGapRangeResponse(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/things", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Query().Get("fail") != "" {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(200)
		},
		Responses: []specout.Response{{Key: "4XX", Type: gapErrBody{}}},
	})
	doc := gapsDocOn(t, d, r)

	resp := gapsOp(t, doc, "/things", "get")["responses"].(map[string]any)
	if _, ok := resp["4XX"]; !ok {
		t.Errorf("responses = %v, want a 4XX entry", resp)
	}
	if _, ok := resp["404"]; ok {
		t.Error("a range key must not fan out into one code")
	}

	declared, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := d.SpecStatuses()
	if err != nil {
		t.Fatal(err)
	}
	key := specout.RouteKey{Method: "GET", Path: "/things"}
	if declared[key][404] {
		t.Error("a range declares no concrete code, so a 404 is not required")
	}
	if !allowed[key][404] {
		t.Error("the 4XX range must allow a 404")
	}

	// end to end: 200 for the required status, 404 for the range, no drift.
	rec := recorder.New(r)
	for _, target := range []string{"/things", "/things?fail=1"} {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
	recorder.Verify(t, d, rec)
}

// TestGapContentTypes: one body shape under several media types, on the
// request and on the response.
func TestGapContentTypes(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Post("/things", specout.Handler[gapQuery, gapRes]{
		HandlerFunc:         func(w http.ResponseWriter, req *http.Request) {},
		RequestContentTypes: []string{"application/json", "application/xml"},
		Responses:           []specout.Response{{Status: 201, ContentTypes: []string{"application/json", "application/xml"}}},
	})
	doc := gapsDocOn(t, d, r)
	op := gapsOp(t, doc, "/things", "post")

	reqContent := op["requestBody"].(map[string]any)["content"].(map[string]any)
	if _, ok := reqContent["application/xml"]; !ok {
		t.Errorf("request content = %v, want xml too", reqContent)
	}
	responses := op["responses"].(map[string]any)
	resContent := responses["201"].(map[string]any)["content"].(map[string]any)
	if len(resContent) != 2 {
		t.Errorf("response content = %v, want two media types", resContent)
	}
	jsonSchema := resContent["application/json"].(map[string]any)["schema"].(map[string]any)
	xmlSchema := resContent["application/xml"].(map[string]any)["schema"].(map[string]any)
	if !strings.Contains(jsonSchema["$ref"].(string), "gapRes") {
		t.Errorf("json schema = %v", jsonSchema)
	}
	if jsonSchema["$ref"] != xmlSchema["$ref"] {
		t.Error("both media types must carry the same schema")
	}
	// the Res default 200 stays: a 201 alone needs Omit on the 200
	if _, ok := responses["200"]; !ok {
		t.Errorf("responses = %v, want the Res default 200 too", responses)
	}
}

// TestGapOAuth2: flows and scopes, and a clear panic on a flow with no URL.
func TestGapOAuth2(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1",
		Auth: []specout.AuthScheme{specout.OAuth2("petstore_auth", map[string]specout.OAuth2Flow{
			"implicit": {
				AuthorizationURL: "https://petstore3.swagger.io/oauth/authorize",
				Scopes:           map[string]string{"write:pets": "modify pets", "read:pets": "read pets"},
			},
		})},
	})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/pets", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) {},
	})
	doc := gapsDocOn(t, d, r)

	scheme := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)["petstore_auth"].(map[string]any)
	if scheme["type"] != "oauth2" {
		t.Fatalf("scheme = %v", scheme)
	}
	implicit := scheme["flows"].(map[string]any)["implicit"].(map[string]any)
	if implicit["authorizationUrl"] != "https://petstore3.swagger.io/oauth/authorize" {
		t.Errorf("implicit = %v", implicit)
	}
	scopes := implicit["scopes"].(map[string]any)
	if len(scopes) != 2 || scopes["write:pets"] != "modify pets" {
		t.Errorf("scopes = %v", scopes)
	}

	bad := specout.New(specout.Config{Title: "t", Version: "1",
		Auth: []specout.AuthScheme{specout.OAuth2("x", map[string]specout.OAuth2Flow{"implicit": {}})}})
	defer func() {
		if r := recover(); r == nil {
			t.Error("an implicit flow with no AuthorizationURL must panic")
		}
	}()
	gapsDocOn(t, bad, chi.NewRouter())
}

// TestGapParamStyle: style= and explode= reach the parameter object.
func TestGapParamStyle(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/things", specout.Handler[gapQuery, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) {},
	})
	doc := gapsDocOn(t, d, r)

	got := map[string]map[string]any{}
	for _, p := range gapsOp(t, doc, "/things", "get")["parameters"].([]any) {
		m := p.(map[string]any)
		got[m["name"].(string)] = m
	}
	if got["filter"]["style"] != "deepObject" || got["filter"]["explode"] != true {
		t.Errorf("filter = %v", got["filter"])
	}
	if got["ids"]["style"] != "form" || got["ids"]["explode"] != false {
		t.Errorf("ids = %v", got["ids"])
	}
	// a parameter keyword, not a schema keyword: the schema must not grow one
	if sch := got["filter"]["schema"].(map[string]any); sch["style"] != nil || sch["explode"] != nil {
		t.Errorf("style/explode belong to the parameter, not its schema: %v", sch)
	}
}

// TestGapDocFields: contact/license/termsOfService, operation and tag
// externalDocs, operation x- extensions.
func TestGapDocFields(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1", TermsOfService: "https://x.example/tos",
		Contact: &specout.Contact{Name: "API team", Email: "api@x.example"},
		License: &specout.License{Name: "MIT", URL: "https://x.example/mit"},
		Tags:    []specout.Tag{{Name: "things", ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/things"}}},
	})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/things", specout.Handler[struct{}, gapRes]{
		HandlerFunc:  func(w http.ResponseWriter, req *http.Request) {},
		Tags:         []string{"things"},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/op", Description: "how it works"},
		Raw:          map[string]any{"x-rate-limit": 100},
	})
	doc := gapsDocOn(t, d, r)

	info := doc["info"].(map[string]any)
	if info["termsOfService"] != "https://x.example/tos" {
		t.Errorf("termsOfService = %v", info["termsOfService"])
	}
	contact := info["contact"].(map[string]any)
	if contact["email"] != "api@x.example" {
		t.Errorf("contact = %v", contact)
	}
	if _, ok := contact["url"]; ok {
		t.Error("an empty contact field must not be emitted")
	}
	if info["license"].(map[string]any)["name"] != "MIT" {
		t.Errorf("license = %v", info["license"])
	}

	tag := doc["tags"].([]any)[0].(map[string]any)
	if tag["externalDocs"].(map[string]any)["url"] != "https://x.example/things" {
		t.Errorf("tag externalDocs = %v", tag["externalDocs"])
	}
	op := gapsOp(t, doc, "/things", "get")
	if op["externalDocs"].(map[string]any)["description"] != "how it works" {
		t.Errorf("operation externalDocs = %v", op["externalDocs"])
	}
	if op["x-rate-limit"] != float64(100) {
		t.Errorf("x-rate-limit = %v", op["x-rate-limit"])
	}
}

// TestGapSchemaTags: deprecated, jsonschema_extras and repeated example=
// tags, the three property-level shapes the examples gap table lists.
func TestGapSchemaTags(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/things", specout.Handler[struct{}, gapBody]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) {},
	})
	doc := gapsDocOn(t, d, r)
	props := doc["components"].(map[string]any)["schemas"].(map[string]any)["gapBody"].(map[string]any)["properties"].(map[string]any)

	if props["old"].(map[string]any)["deprecated"] != true {
		t.Errorf("old = %v, want deprecated", props["old"])
	}
	if props["tagged"].(map[string]any)["x-ms-identifiers"] != "id" {
		t.Errorf("tagged = %v, want the x- extension", props["tagged"])
	}
	if ex := props["example"].(map[string]any)["examples"].([]any); len(ex) != 2 {
		t.Errorf("examples = %v, want two", props["example"])
	}
}

// TestGapRangeCode: naming one code beside a range makes that code a
// coverage expectation; a bare range key requires nothing.
func TestGapRangeCode(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/partial", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(404) },
		Responses: []specout.Response{
			{Status: 404, Key: "4XX", Raw: map[string]any{"description": "not found"}},
			{Status: 200, Omit: true},
		},
	})
	doc := gapsDocOn(t, d, r)
	responses := gapsOp(t, doc, "/partial", "get")["responses"].(map[string]any)
	if responses["4XX"].(map[string]any)["description"] != "not found" {
		t.Errorf("4XX = %v", responses["4XX"])
	}
	if _, ok := responses["200"]; ok {
		t.Errorf("responses = %v, Omit must drop the Res default", responses)
	}
	declared, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	key := specout.RouteKey{Method: "GET", Path: "/partial"}
	if !declared[key][404] {
		t.Error("the code named beside the range must be required")
	}
	if declared[key][200] {
		t.Error("Omit must drop the Res default from coverage")
	}
}

// TestGapExternalDocsNeedsURL: url is required by OpenAPI, and an empty one
// used to emit an object the validator rejects.
func TestGapExternalDocsNeedsURL(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1",
		ExternalDocs: &specout.ExternalDocs{Description: "no link"}})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/things", specout.Handler[struct{}, gapRes]{
		HandlerFunc: func(w http.ResponseWriter, req *http.Request) {},
	})
	defer func() {
		if recover() == nil {
			t.Error("ExternalDocs without a URL must panic")
		}
	}()
	gapsDocOn(t, d, r)
}
