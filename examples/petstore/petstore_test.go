package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout/adapters/specoutchi"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/internal/examplekit/exampletest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func spec(t *testing.T) map[string]any {
	t.Helper()
	d, _ := New()
	return exampletest.Spec(t, d)
}

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
		for m, wantID := range methods {
			op, hasOp := item[m].(map[string]any)
			if !assert.True(t, hasOp, "missing %s %s", m, p) {
				continue
			}
			assert.Equal(t, wantID, op["operationId"], "%s %s operationId", m, p)
		}
	}
}

func TestSpecKeepsPublishedShapes(t *testing.T) {
	doc := spec(t)

	param := dig(t, doc, "paths", "/pet/{petId}", "get", "parameters").([]any)[0].(map[string]any)
	assert.Equal(t, "petId", param["name"])
	assert.Equal(t, "integer", dig(t, param, "schema", "type"), "petId param")
	assert.Equal(t, "int64", dig(t, param, "schema", "format"), "petId param")

	headers := dig(t, doc, "paths", "/user/login", "get", "responses", "200", "headers").(map[string]any)
	assert.Equal(t, "integer", dig(t, headers, "X-Rate-Limit", "schema", "type"))
	assert.Equal(t, "date-time", dig(t, headers, "X-Expires-After", "schema", "format"))

	inv := dig(t, doc, "paths", "/store/inventory", "get", "responses", "200", "content", "application/json", "schema").(map[string]any)
	assert.Equal(t, "object", inv["type"], "inventory schema")
	assert.NotNil(t, inv["additionalProperties"], "inventory schema")

	assert.Contains(t, dig(t, doc, "paths", "/user/logout", "get").(map[string]any), "security", "logout should carry security: []")
	assert.NotContains(t, dig(t, doc, "paths", "/pet/{petId}", "get").(map[string]any), "security", "getPetById should inherit the global requirement")

	content := dig(t, doc, "paths", "/pet/{petId}/uploadImage", "post", "requestBody", "content").(map[string]any)
	require.Contains(t, content, "multipart/form-data", "uploadImage content")

	schemas := dig(t, doc, "components", "schemas").(map[string]any)
	assert.NotContains(t, schemas, "File", "File marker leaked into components.schemas")
	req := dig(t, content, "multipart/form-data", "schema").(map[string]any)
	assert.Equal(t, []any{"file"}, req["required"], "uploadImageReq required")
	assert.Contains(t, dig(t, req, "properties", "file").(map[string]any), "format")

	for _, name := range []string{"Pet", "Tag", "Category"} {
		assert.Equal(t, "int64", dig(t, schemas, name, "properties", "id", "format"), "%s.id format", name)
	}

	for p, item := range dig(t, doc, "paths").(map[string]any) {
		for m, op := range item.(map[string]any) {
			responses := dig(t, op, "responses").(map[string]any)
			if def, ok := responses["default"]; ok {
				assert.Equal(t, map[string]any{"description": "Unexpected error"}, def, "%s %s default", m, p)
			}
		}
	}
}

func TestDemoServesEveryRouteAndNoUndocumentedCode(t *testing.T) {
	rec := recorder.New(nil, "/openapi.json")
	d, r := New(specoutchi.Record(rec))
	srv := httptest.NewServer(Handler(r))
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
	do("HEAD", "/pet/2", "", 405)
	do("GET", "/openapi.json", "", 200)
	do("GET", "/nope", "", 404)

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

	errs := adaptertest.Errs(recorder.Verify(d, rec))
	for _, e := range errs {
		assert.NotContains(t, e, "but spec does not declare it", "drift")
	}
	if len(errs) == 0 {
		t.Log("no drift at all: the demo produced every declared code")
	}
}

func TestMalformedBodyCannotRunAction(t *testing.T) {
	for _, body := range []string{`{"name":"changed"}{}`, `{"name":"changed"}null`, `{"name":"changed"} garbage`, ""} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/pet", strings.NewReader(body))
		w := httptest.NewRecorder()
		called := false
		withBody(w, r, "invalid JSON", func(map[string]any) { called = true })
		assert.False(t, called, "invalid bodies must not reach the store action")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}
