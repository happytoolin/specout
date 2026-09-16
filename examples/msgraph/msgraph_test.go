package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

// publishedGraph is the path -> method set of the eight operations this
// example takes from the live document (graph.microsoft.com/v1.0, 11546
// paths). Every deviation is listed in examples/README.md.
var publishedGraph = map[string][]string{
	"/me":                           {"get"},
	"/users":                        {"get", "post"},
	"/users/{user-id}":              {"get", "patch", "delete"},
	"/users/{user-id}/photo/$value": {"get"},
	"/users/{user-id}/sendMail":     {"post"},
}

var publishedGraphIDs = []string{
	"me.user.GetUser",
	"users.user.ListUser",
	"users.user.CreateUser",
	"users.user.GetUser",
	"users.user.UpdateUser",
	"users.user.DeleteUser",
	"users.GetPhotoContent",
	"users.user.sendMail",
}

func buildGraphSpec(t *testing.T) map[string]any {
	t.Helper()
	d, _ := New()
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

func graphOp(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("no paths object")
	}
	item, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("missing path %s", path)
	}
	op, ok := item[method].(map[string]any)
	if !ok {
		t.Fatalf("missing %s %s", method, path)
	}
	return op
}

func paramNamed(t *testing.T, op map[string]any, name, in string) map[string]any {
	t.Helper()
	raw, _ := op["parameters"].([]any)
	for _, one := range raw {
		p := one.(map[string]any)
		if p["name"] == name && p["in"] == in {
			return p
		}
	}
	t.Fatalf("missing %s parameter %s", in, name)
	return nil
}

func TestSpecMatchesPublishedDocument(t *testing.T) {
	doc := buildGraphSpec(t)
	paths := doc["paths"].(map[string]any)
	if len(paths) != len(publishedGraph) {
		t.Errorf("paths = %d, want %d", len(paths), len(publishedGraph))
	}
	ids := []string{}
	for p, methods := range publishedGraph {
		item, ok := paths[p].(map[string]any)
		if !ok {
			t.Errorf("missing path %s", p)
			continue
		}
		if len(item) != len(methods) {
			t.Errorf("%s has %d operations, want %d", p, len(item), len(methods))
		}
		for _, m := range methods {
			op, ok := item[m].(map[string]any)
			if !ok {
				t.Errorf("missing %s %s", m, p)
				continue
			}
			id, _ := op["operationId"].(string)
			if id == "" {
				t.Errorf("%s %s has no operationId", m, p)
			}
			ids = append(ids, id)
		}
	}
	if len(ids) != 8 {
		t.Errorf("operations = %d, want 8", len(ids))
	}
	for _, want := range publishedGraphIDs {
		found := false
		for _, id := range ids {
			found = found || id == want
		}
		if !found {
			t.Errorf("missing operationId %s", want)
		}
	}
}

// TestSpecKeepsPublishedShapes pins the Graph shapes the example exists to
// prove: the shared OData error envelope keyed by code, a hyphenated path
// parameter name, header parameters, OData dollar-prefixed query parameters,
// a binary media response, and a request body split out of Req.
func TestSpecKeepsPublishedShapes(t *testing.T) {
	doc := buildGraphSpec(t)
	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// the published document declares no securitySchemes
	if _, ok := comps["securitySchemes"]; ok {
		t.Error("the published document has no securitySchemes")
	}

	// every operation carries the ODataError envelope, keyed by code
	for path, methods := range publishedGraph {
		for _, m := range methods {
			responses := graphOp(t, doc, path, m)["responses"].(map[string]any)
			for _, code := range []string{"400", "401", "403", "404", "429", "500"} {
				resp, ok := responses[code].(map[string]any)
				if !ok {
					t.Errorf("%s %s: missing %s", m, path, code)
					continue
				}
				media := resp["content"].(map[string]any)["application/json"].(map[string]any)
				schema := media["schema"].(map[string]any)
				if schema["$ref"] != "#/components/schemas/ODataError" {
					t.Errorf("%s %s %s schema = %v", m, path, code, schema)
				}
			}
		}
	}

	// the envelope, including the hyphenated innerError property names
	inner := schemas["InnerError"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"request-id", "client-request-id", "date"} {
		if _, ok := inner[name]; !ok {
			t.Errorf("InnerError is missing %s", name)
		}
	}

	// a hyphenated path template name keeps its published spelling
	pathParam := paramNamed(t, graphOp(t, doc, "/users/{user-id}", "get"), "user-id", "path")
	if pathParam["required"] != true {
		t.Errorf("user-id param = %v", pathParam)
	}
	if pathParam["description"] != "The unique identifier of user" {
		t.Errorf("user-id description = %v", pathParam["description"])
	}

	// ConsistencyLevel is a header, not a query parameter
	cl := paramNamed(t, graphOp(t, doc, "/me", "get"), "ConsistencyLevel", "header")
	if cl["required"] != false {
		t.Errorf("ConsistencyLevel required = %v", cl["required"])
	}

	// OData query parameters: $select is an array, $top is a bounded integer
	list := graphOp(t, doc, "/users", "get")
	selectSchema := paramNamed(t, list, "$select", "query")["schema"].(map[string]any)
	if selectSchema["type"] != "array" {
		t.Errorf("$select schema = %v", selectSchema)
	}
	topSchema := paramNamed(t, list, "$top", "query")["schema"].(map[string]any)
	if topSchema["minimum"] != float64(0) {
		t.Errorf("$top minimum = %v", topSchema["minimum"])
	}
	examples, _ := topSchema["examples"].([]any)
	if len(examples) != 1 || examples[0] != float64(50) {
		t.Errorf("$top examples = %v", topSchema["examples"])
	}

	// DELETE: an If-Match header and a description-only 204
	del := graphOp(t, doc, "/users/{user-id}", "delete")
	ifMatch := paramNamed(t, del, "If-Match", "header")
	if ifMatch["schema"].(map[string]any)["type"] != "string" {
		t.Errorf("If-Match = %v", ifMatch)
	}
	resp204 := del["responses"].(map[string]any)["204"].(map[string]any)
	if _, hasContent := resp204["content"]; hasContent {
		t.Errorf("204 should carry no content: %v", resp204)
	}

	// media response: application/octet-stream, binary
	photo := graphOp(t, doc, "/users/{user-id}/photo/$value", "get")
	photo200 := photo["responses"].(map[string]any)["200"].(map[string]any)
	content := photo200["content"].(map[string]any)
	if _, ok := content["application/octet-stream"]; !ok {
		t.Fatalf("photo content = %v", content)
	}
	binary := content["application/octet-stream"].(map[string]any)["schema"].(map[string]any)
	if binary["type"] != "string" || binary["format"] != "binary" {
		t.Errorf("photo schema = %v", binary)
	}

	// sendMail: the published path parameter plus a body split into its own
	// component, under the published property names
	mail := graphOp(t, doc, "/users/{user-id}/sendMail", "post")
	paramNamed(t, mail, "user-id", "path")
	bodyRef := mail["requestBody"].(map[string]any)["content"].(map[string]any)
	ref := bodyRef["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"].(string)
	body := schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"Message", "SaveToSentItems"} {
		if _, ok := body[name]; !ok {
			t.Errorf("sendMail body is missing %s: %v", name, body)
		}
	}
	if _, leaked := body["user-id"]; leaked {
		t.Error("the path parameter must not leak into the body component")
	}

	// the OData collection annotations, and the enum body field
	collection := schemas["UserCollectionResponse"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"@odata.count", "@odata.nextLink", "value"} {
		if _, ok := collection[name]; !ok {
			t.Errorf("UserCollectionResponse is missing %s", name)
		}
	}
	item := schemas["ItemBody"].(map[string]any)["properties"].(map[string]any)["contentType"].(map[string]any)
	enum, _ := item["enum"].([]any)
	if len(enum) != 2 || enum[0] != "text" || enum[1] != "html" {
		t.Errorf("contentType enum = %v", item["enum"])
	}
}

// TestGraphDemoServesEveryRouteAndNoUndocumentedCode drives the gorilla demo
// over real HTTP and checks the drift direction that must hold: every code a
// handler wrote is declared for that route. This example mirrors a published
// document whose error codes an in-memory demo cannot reach (401, 403, 429,
// 500), so the reverse direction may stay unproduced.
func TestGraphDemoServesEveryRouteAndNoUndocumentedCode(t *testing.T) {
	d, r := New()
	rec := recorder.New(r, specout.Skip("/openapi.json"))
	srv := httptest.NewServer(Handler(d, rec))
	defer srv.Close()

	do := func(method, path, body string, want int) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s %s = %d, want %d", method, path, resp.StatusCode, want)
		}
	}

	alice := "48d31887-5fad-4d73-a9f5-3c356e68a038"
	do("GET", "/me", "", 200)
	do("GET", "/users", "", 200)
	do("GET", "/users?$top=1&$select=id,displayName", "", 200)
	do("POST", "/users", "{\"displayName\":\"Carol\"}", 200)
	do("GET", "/users/"+alice, "", 200)
	do("GET", "/users/"+alice+"?$expand=photo", "", 200)
	do("PATCH", "/users/"+alice, "{\"jobTitle\":\"Lead\"}", 200)
	do("GET", "/users/"+alice+"/photo/$value", "", 200)
	do("POST", "/users/"+alice+"/sendMail", "{\"Message\":{\"subject\":\"hi\"},\"SaveToSentItems\":true}", 204)
	do("DELETE", "/users/"+alice, "", 204)

	// the declared 404 and 400 a handler can reach
	do("GET", "/users/nope", "", 404)
	do("POST", "/users/nope/sendMail", "{}", 404)
	do("GET", "/users/nope/photo/$value", "", 404)
	do("POST", "/users", "{bad", 400)

	// an undeclared method on a declared route: gorilla answers 405, which is
	// no operation, so the recorder must key nothing for it
	do("HEAD", "/users/"+alice, "", 405)

	// the spec is served, and its own request is a skip
	sresp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	sresp.Body.Close()
	if sresp.StatusCode != 200 {
		t.Errorf("spec = %d, want 200", sresp.StatusCode)
	}

	// an unmatched path is not drift: the recorder keys nothing for it
	do("GET", "/nope", "", 404)

	ft := &captureT{}
	recorder.Verify(ft, d, rec)
	for _, e := range ft.errs {
		if strings.Contains(e, "but spec does not declare it") {
			t.Errorf("drift: %s", e)
		}
	}
	if len(ft.errs) == 0 {
		t.Log("no drift at all: the demo produced every declared code")
	}
}

// captureT collects failures so a test can assert on one drift category
// without the other failing it.
type captureT struct{ errs []string }

func (c *captureT) Helper()                   {}
func (c *captureT) Errorf(f string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(f, a...)) }
func (c *captureT) Fatalf(f string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(f, a...)) }
