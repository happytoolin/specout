package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit/exampletest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// published is the path -> method -> operationId map of the live document
// (petstore.swagger.io/v3/openapi.json, 3.0.4). The example must emit it
// exactly; every deviation is listed in examples/README.md.
var published = map[string]map[string]string{
	"/pet":                     {"put": "updatePet", "post": "addPet"},
	"/pet/findByStatus":        {"get": "findPetsByStatus"},
	"/pet/findByTags":          {"get": "findPetsByTags"},
	"/pet/{petId}":             {"get": "getPetById", "post": "updatePetWithForm", "delete": "deletePet"},
	"/pet/{petId}/uploadImage": {"post": "uploadFile"},
	"/store/inventory":         {"get": "getInventory"},
	"/store/order":             {"post": "placeOrder"},
	"/store/order/{orderId}":   {"get": "getOrderById", "delete": "deleteOrder"},
	"/user":                    {"post": "createUser"},
	"/user/createWithList":     {"post": "createUsersWithListInput"},
	"/user/login":              {"get": "loginUser"},
	"/user/logout":             {"get": "logoutUser"},
	"/user/{username}":         {"get": "getUserByName", "put": "updateUser", "delete": "deleteUser"},
}

// spec builds the example and returns its document.
func spec(t *testing.T) map[string]any {
	t.Helper()
	d, _ := New()
	return exampletest.Spec(t, d)
}

// dig walks a decoded document by key path: the shared example walker.
var dig = exampletest.Dig

func TestSpecMatchesPublishedDocument(t *testing.T) {
	paths := dig(t, spec(t), "paths").(map[string]any)
	assert.Len(t, paths, len(published), "paths")
	for p, methods := range published {
		item, ok := paths[p].(map[string]any)
		if !assert.True(t, ok, "missing path %s", p) {
			continue
		}
		assert.Len(t, item, len(methods), "%s operations", p)
		// the 19 published operationIds, no more, no fewer, on the right route
		for m, wantID := range methods {
			op, ok := item[m].(map[string]any)
			if !assert.True(t, ok, "missing %s %s", m, p) {
				continue
			}
			assert.Equal(t, wantID, op["operationId"], "%s %s operationId", m, p)
		}
	}
}

// TestSpecKeepsPublishedShapes pins the shapes the example exists to prove:
// typed path params, response headers, an inlined map body, security opt-out,
// and the answer to the two bugs the example found (leaked File component,
// required carrying the Go field name).
func TestSpecKeepsPublishedShapes(t *testing.T) {
	doc := spec(t)

	// int64 path param with its published description
	param := dig(t, doc, "paths", "/pet/{petId}", "get", "parameters").([]any)[0].(map[string]any)
	assert.Equal(t, "petId", param["name"])
	assert.Equal(t, "integer", dig(t, param, "schema", "type"), "petId param")
	assert.Equal(t, "int64", dig(t, param, "schema", "format"), "petId param")

	// response headers, scalar and time
	headers := dig(t, doc, "paths", "/user/login", "get", "responses", "200", "headers").(map[string]any)
	assert.Equal(t, "integer", dig(t, headers, "X-Rate-Limit", "schema", "type"))
	assert.Equal(t, "date-time", dig(t, headers, "X-Expires-After", "schema", "format"))

	// map[string]int32 body is inlined, not $ref'd
	inv := dig(t, doc, "paths", "/store/inventory", "get", "responses", "200", "content", "application/json", "schema").(map[string]any)
	assert.Equal(t, "object", inv["type"], "inventory schema")
	assert.NotNil(t, inv["additionalProperties"], "inventory schema")

	// security opt-out on a published public operation, none on a protected one
	assert.Contains(t, dig(t, doc, "paths", "/user/logout", "get").(map[string]any), "security", "logout should carry security: []")
	assert.NotContains(t, dig(t, doc, "paths", "/pet/{petId}", "get").(map[string]any), "security", "getPetById should inherit the global requirement")

	// uploadImage: multipart with the renamed, required file field
	content := dig(t, doc, "paths", "/pet/{petId}/uploadImage", "post", "requestBody", "content").(map[string]any)
	require.Contains(t, content, "multipart/form-data", "uploadImage content")

	// the File marker must not leak into components, and required must name
	// the property, not the Go field
	schemas := dig(t, doc, "components", "schemas").(map[string]any)
	assert.NotContains(t, schemas, "File", "File marker leaked into components.schemas")
	req := schemas["uploadImageReqBody"].(map[string]any)
	assert.Equal(t, []any{"file"}, req["required"], "uploadImageReq required")
	assert.Contains(t, dig(t, req, "properties", "file").(map[string]any), "format")

	// Tag and Category are reached only through Pet, so invopop hoists them
	// without a Go-type entry: the property fixups must follow the $ref into
	// the nested component. Regression — the format= tag used to survive on
	// Pet but vanish on Tag.
	for _, name := range []string{"Pet", "Tag", "Category"} {
		assert.Equal(t, "int64", dig(t, schemas, name, "properties", "id", "format"), "%s.id format", name)
	}
}

// captureT collects failures so a test can assert on one drift category
// without the other failing it.
type captureT = exampletest.CaptureT

// TestDemoServesEveryRouteAndNoUndocumentedCode drives the served demo over
// real HTTP, then checks the drift direction that must hold: every code a
// handler wrote is declared for that route. The other direction (declared but
// not produced) cannot hold here: the example mirrors a published document
// whose error codes an in-memory demo has no path to reach.
func TestDemoServesEveryRouteAndNoUndocumentedCode(t *testing.T) {
	d, r := New()
	rec := recorder.New(r, specout.Skip("/openapi.json"))
	srv := httptest.NewServer(Handler(rec))
	defer srv.Close()

	send := func(req *http.Request, want int) {
		t.Helper()
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, want, resp.StatusCode, "%s %s", req.Method, req.URL.Path)
	}
	do := func(method, path, body string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+"/api/v3"+path, strings.NewReader(body))
		require.NoError(t, err)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		send(req, want)
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
	// the spec is served under the declared server prefix
	do("GET", "/openapi.json", "", 200)
	// an unmatched path is not drift: the recorder keys nothing for it
	do("GET", "/nope", "", 404)

	// multipart upload: the one body the example does not send as JSON
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "pet.png")
	require.NoError(t, err)
	_, err = fw.Write([]byte("png"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/api/v3/pet/2/uploadImage", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	send(req, 200)

	ft := &captureT{}
	recorder.Verify(ft, d, rec)
	for _, e := range ft.Errs {
		assert.NotContains(t, e, "but spec does not declare it", "drift")
	}
	if len(ft.Errs) == 0 {
		t.Log("no drift at all: the demo produced every declared code")
	}
}
