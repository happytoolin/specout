package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

// published is the path -> method set of the live document
// (petstore.swagger.io/v3/openapi.json, 3.0.4). The example must emit it
// exactly; every deviation is listed in examples/README.md.
var published = map[string][]string{
	"/pet":                     {"put", "post"},
	"/pet/findByStatus":        {"get"},
	"/pet/findByTags":          {"get"},
	"/pet/{petId}":             {"get", "post", "delete"},
	"/pet/{petId}/uploadImage": {"post"},
	"/store/inventory":         {"get"},
	"/store/order":             {"post"},
	"/store/order/{orderId}":   {"get", "delete"},
	"/user":                    {"post"},
	"/user/createWithList":     {"post"},
	"/user/login":              {"get"},
	"/user/logout":             {"get"},
	"/user/{username}":         {"get", "put", "delete"},
}

func buildSpec(t *testing.T) map[string]any {
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

func pathItem(t *testing.T, doc map[string]any, path string) map[string]any {
	t.Helper()
	p, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("no paths object")
	}
	item, ok := p[path].(map[string]any)
	if !ok {
		t.Fatalf("missing path %s", path)
	}
	return item
}

func operation(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	op, ok := pathItem(t, doc, path)[method].(map[string]any)
	if !ok {
		t.Fatalf("missing %s %s", method, path)
	}
	return op
}

func TestSpecMatchesPublishedDocument(t *testing.T) {
	doc := buildSpec(t)
	paths := doc["paths"].(map[string]any)
	if len(paths) != len(published) {
		t.Errorf("paths = %d, want %d", len(paths), len(published))
	}
	for p, methods := range published {
		item, ok := paths[p].(map[string]any)
		if !ok {
			t.Errorf("missing path %s", p)
			continue
		}
		for _, m := range methods {
			if _, ok := item[m]; !ok {
				t.Errorf("missing %s %s", m, p)
			}
		}
		if len(item) != len(methods) {
			t.Errorf("%s has %d operations, want %d", p, len(item), len(methods))
		}
	}
	// the 19 published operationIds, no more, no fewer
	ids := []string{}
	for p, raw := range paths {
		for m, op := range raw.(map[string]any) {
			id, _ := op.(map[string]any)["operationId"].(string)
			if id == "" {
				t.Errorf("%s %s has no operationId", m, p)
			}
			ids = append(ids, id)
		}
	}
	if len(ids) != 19 {
		t.Errorf("operations = %d, want 19", len(ids))
	}
	for _, want := range []string{"updatePet", "addPet", "findPetsByStatus", "findPetsByTags",
		"getPetById", "updatePetWithForm", "deletePet", "uploadFile", "getInventory",
		"placeOrder", "getOrderById", "deleteOrder", "createUser", "createUsersWithListInput",
		"loginUser", "logoutUser", "getUserByName", "updateUser", "deleteUser"} {
		found := false
		for _, id := range ids {
			found = found || id == want
		}
		if !found {
			t.Errorf("missing operationId %s", want)
		}
	}
}

// TestSpecKeepsPublishedShapes pins the shapes the example exists to prove:
// typed path params, response headers, an inlined map body, security opt-out,
// and the answer to the two bugs the example found (leaked File component,
// required carrying the Go field name).
func TestSpecKeepsPublishedShapes(t *testing.T) {
	doc := buildSpec(t)

	// int64 path param with its published description
	params := operation(t, doc, "/pet/{petId}", "get")["parameters"].([]any)
	first := params[0].(map[string]any)
	schema := first["schema"].(map[string]any)
	if first["name"] != "petId" || schema["type"] != "integer" || schema["format"] != "int64" {
		t.Errorf("petId param = %v", first)
	}

	// response headers, scalar and time
	resp200 := operation(t, doc, "/user/login", "get")["responses"].(map[string]any)["200"].(map[string]any)
	headers := resp200["headers"].(map[string]any)
	if headers["X-Rate-Limit"].(map[string]any)["schema"].(map[string]any)["type"] != "integer" {
		t.Errorf("X-Rate-Limit = %v", headers["X-Rate-Limit"])
	}
	if headers["X-Expires-After"].(map[string]any)["schema"].(map[string]any)["format"] != "date-time" {
		t.Errorf("X-Expires-After = %v", headers["X-Expires-After"])
	}

	// map[string]int32 body is inlined, not $ref'd
	inv := operation(t, doc, "/store/inventory", "get")["responses"].(map[string]any)["200"].(map[string]any)
	invSchema := inv["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if invSchema["type"] != "object" || invSchema["additionalProperties"] == nil {
		t.Errorf("inventory schema = %v", invSchema)
	}

	// security opt-out on a published public operation, none on a protected one
	if _, ok := operation(t, doc, "/user/logout", "get")["security"]; !ok {
		t.Error("logout should carry security: []")
	}
	if _, ok := operation(t, doc, "/pet/{petId}", "get")["security"]; ok {
		t.Error("getPetById should inherit the global requirement")
	}

	// uploadImage: multipart with the renamed, required file field
	body := operation(t, doc, "/pet/{petId}/uploadImage", "post")["requestBody"].(map[string]any)
	content := body["content"].(map[string]any)
	if _, ok := content["multipart/form-data"]; !ok {
		t.Fatalf("uploadImage content = %v", content)
	}

	// the File marker must not leak into components, and required must name
	// the property, not the Go field
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	if _, leaked := schemas["File"]; leaked {
		t.Error("File marker leaked into components.schemas")
	}
	req := schemas["uploadImageReq"].(map[string]any)
	required := req["required"].([]any)
	if len(required) != 1 || required[0] != "file" {
		t.Errorf("uploadImageReq required = %v, want [file]", required)
	}
	if _, ok := req["properties"].(map[string]any)["file"].(map[string]any)["format"]; !ok {
		t.Errorf("file field = %v", req["properties"])
	}
}

// captureT collects failures so a test can assert on one drift category
// without the other failing it.
type captureT struct{ errs []string }

func (c *captureT) Helper()                   {}
func (c *captureT) Errorf(f string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(f, a...)) }
func (c *captureT) Fatalf(f string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(f, a...)) }

// TestDemoServesEveryRouteAndNoUndocumentedCode drives the served demo over
// real HTTP, then checks the drift direction that must hold: every code a
// handler wrote is declared for that route. The other direction (declared but
// not produced) cannot hold here: the example mirrors a published document
// whose error codes an in-memory demo has no path to reach.
func TestDemoServesEveryRouteAndNoUndocumentedCode(t *testing.T) {
	d, r := New()
	rec := recorder.New(r, specout.Skip("/openapi.json"))
	srv := httptest.NewServer(Handler(d, rec))
	defer srv.Close()

	do := func(method, path, body string, want int) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+"/api/v3"+path, strings.NewReader(body))
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

	do("PUT", "/pet", `{"name":"doggie","photoUrls":["a"],"status":"available"}`, 200)
	do("POST", "/pet", `{"name":"rex","photoUrls":["b"]}`, 200)
	do("GET", "/pet/1", "", 200)
	do("GET", "/pet/999", "", 404)
	do("GET", "/pet/findByStatus?status=available", "", 200)
	do("GET", "/pet/findByTags?tags=dogs", "", 200)
	do("POST", "/pet/1?name=renamed&status=pending", "", 200)
	do("DELETE", "/pet/1", "", 200)
	do("GET", "/store/inventory", "", 200)
	do("POST", "/store/order", `{"petId":2,"quantity":1}`, 200)
	do("GET", "/store/order/3", "", 200)
	do("DELETE", "/store/order/3", "", 200)
	do("POST", "/user", `{"username":"u1","password":"p"}`, 200)
	do("POST", "/user/createWithList", `[{"username":"u2"}]`, 200)
	do("GET", "/user/login?username=u1&password=p", "", 200)
	do("GET", "/user/logout", "", 200)
	do("GET", "/user/u1", "", 200)
	do("PUT", "/user/u1", `{"username":"u1","email":"e@x"}`, 200)
	do("DELETE", "/user/u1", "", 200)
	// chi answers HEAD on a GET-only route with 405: not an operation, so
	// the recorder must key nothing for it (no drift either way)
	do("HEAD", "/pet/2", "", 405)

	// multipart upload: the one body the example does not send as JSON
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "pet.png")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("png"))
	mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v3/pet/2/uploadImage", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("uploadImage = %d, want 200", resp.StatusCode)
	}

	// the spec is served under the declared server prefix
	sresp, err := http.Get(srv.URL + "/api/v3/openapi.json")
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
