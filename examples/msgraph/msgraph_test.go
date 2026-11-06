package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout/adapters/specoutgorilla"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/internal/examplekit/exampletest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var publishedGraph = map[string]map[string]string{
	"/me":                           {"get": "me.user.GetUser"},
	"/users":                        {"get": "users.user.ListUser", "post": "users.user.CreateUser"},
	"/users/{user-id}":              {"get": "users.user.GetUser", "patch": "users.user.UpdateUser", "delete": "users.user.DeleteUser"},
	"/users/{user-id}/photo/$value": {"get": "users.GetPhotoContent"},
	"/users/{user-id}/sendMail":     {"post": "users.user.sendMail"},
}

func spec(t *testing.T) map[string]any {
	t.Helper()
	d, _ := New()
	return exampletest.Spec(t, d)
}

var dig = exampletest.Obj

func graphOp(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	return dig(t, doc, "paths", path, method)
}

func paramNamed(t *testing.T, op map[string]any, name, in string) map[string]any {
	t.Helper()
	raw, _ := op["parameters"].([]any)
	for _, one := range raw {
		if p := one.(map[string]any); p["name"] == name && p["in"] == in {
			return p
		}
	}
	require.Fail(t, "missing parameter", "%s parameter %s", in, name)
	return nil
}

func haveKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		assert.Contains(t, m, k)
	}
}

func TestSpecMatchesPublishedDocument(t *testing.T) {
	paths := dig(t, spec(t), "paths")
	assert.Len(t, paths, len(publishedGraph), "paths")
	ids := 0
	for p, methods := range publishedGraph {
		item := dig(t, paths, p)
		assert.Len(t, item, len(methods), "%s operations", p)
		for m, id := range methods {
			assert.Equal(t, id, dig(t, item, m)["operationId"], "%s %s operationId", p, m)
			ids++
		}
	}
	assert.Equal(t, 8, ids, "operation count")
}

func TestSpecKeepsPublishedShapes(t *testing.T) {
	doc := spec(t)
	comps := dig(t, doc, "components")
	schemas := dig(t, comps, "schemas")

	assert.NotContains(t, comps, "securitySchemes", "components")

	for path, methods := range publishedGraph {
		for m := range methods {
			responses := dig(t, graphOp(t, doc, path, m), "responses")
			for _, code := range []string{"4XX", "5XX"} {
				resp := dig(t, responses, code)
				assert.Equal(t, "error", resp["description"], "%s %s %s description", m, path, code)
				assert.Equal(t, "#/components/schemas/ODataError",
					dig(t, resp, "content", "application/json", "schema")["$ref"], "%s %s %s schema", m, path, code)
			}
			if _, ok := responses["2XX"]; !ok {
				assert.Contains(t, responses, "204", "%s %s: neither a 2XX nor a 204 response", m, path)
			}
			assert.NotContains(t, responses, "404", m+" "+path)
		}
	}

	haveKeys(t, dig(t, schemas, "InnerError", "properties"), "request-id", "client-request-id", "date")

	arms, _ := dig(t, schemas, "MainError", "properties", "innerError")["anyOf"].([]any)
	require.Len(t, arms, 2, "MainError.innerError = %v, want a $ref and a null arm", arms)
	assert.Equal(t, "null", arms[1].(map[string]any)["type"])

	me := graphOp(t, doc, "/me", "get")
	list := graphOp(t, doc, "/users", "get")
	del := graphOp(t, doc, "/users/{user-id}", "delete")
	mail := graphOp(t, doc, "/users/{user-id}/sendMail", "post")
	photo := graphOp(t, doc, "/users/{user-id}/photo/$value", "get")
	userParam := paramNamed(t, graphOp(t, doc, "/users/{user-id}", "get"), "user-id", "path")
	mailSchema := dig(t, mail, "requestBody", "content", "application/json", "schema")
	bodyProps := dig(t, schemas, strings.TrimPrefix(mailSchema["$ref"].(string), "#/components/schemas/"), "properties")

	for _, c := range []struct {
		label     string
		got, want any
	}{
		{"me externalDocs", dig(t, me, "externalDocs")["url"], "https://learn.microsoft.com/graph/api/user-get?view=graph-rest-1.0"},
		{"user-id required", userParam["required"], true},
		{"user-id description", userParam["description"], "The unique identifier of user"},
		{"ConsistencyLevel required", paramNamed(t, me, "ConsistencyLevel", "header")["required"], false},
		{"$select type", paramNamed(t, list, "$select", "query")["schema"].(map[string]any)["type"], "array"},
		{"$top minimum", paramNamed(t, list, "$top", "query")["schema"].(map[string]any)["minimum"], float64(0)},
		{"$top examples", paramNamed(t, list, "$top", "query")["schema"].(map[string]any)["examples"], []any{float64(50)}},
		{"If-Match type", paramNamed(t, del, "If-Match", "header")["schema"].(map[string]any)["type"], "string"},
		{"photo type", dig(t, photo, "responses", "2XX", "content", "application/octet-stream", "schema")["type"], "string"},
		{"photo format", dig(t, photo, "responses", "2XX", "content", "application/octet-stream", "schema")["format"], "binary"},
		{"contentType enum", dig(t, schemas, "ItemBody", "properties", "contentType")["enum"], []any{"text", "html"}},
	} {
		assert.Equal(t, c.want, c.got, c.label)
	}

	assert.NotContains(t, dig(t, del, "responses", "204"), "content", "DELETE 204")
	paramNamed(t, mail, "user-id", "path")
	haveKeys(t, bodyProps, "Message", "SaveToSentItems")
	assert.NotContains(t, bodyProps, "user-id", "sendMail body")
	haveKeys(t, dig(t, schemas, "UserCollectionResponse", "properties"), "@odata.count", "@odata.nextLink", "value")
}

func TestGraphDemoServesEveryRouteAndNoUndocumentedCode(t *testing.T) {
	rec := recorder.New(nil, "/openapi.json")
	d, r := New(specoutgorilla.Record(rec))
	srv := httptest.NewServer(Handler(r))
	defer srv.Close()

	do := func(method, path, body string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, want, resp.StatusCode, "%s %s", method, path)
	}

	alice := "48d31887-5fad-4d73-a9f5-3c356e68a038"
	for _, c := range []struct {
		m, p, body string
		want       int
	}{
		{"GET", "/me", "", 200},
		{"GET", "/users", "", 200},
		{"GET", "/users?$top=1&$select=id,displayName", "", 200},
		{"POST", "/users", "{\"displayName\":\"Carol\"}", 200},
		{"GET", "/users/" + alice, "", 200},
		{"GET", "/users/" + alice + "?$expand=photo", "", 200},
		{"PATCH", "/users/" + alice, "{\"jobTitle\":\"Lead\"}", 200},
		{"GET", "/users/" + alice + "/photo/$value", "", 200},
		{"POST", "/users/" + alice + "/sendMail", "{\"Message\":{\"subject\":\"hi\"},\"SaveToSentItems\":true}", 204},
		{"DELETE", "/users/" + alice, "", 204},
		{"GET", "/users/nope", "", 404},
		{"POST", "/users/nope/sendMail", "{}", 404},
		{"GET", "/users/nope/photo/$value", "", 404},
		{"POST", "/users", "{bad", 400},
		{"HEAD", "/users/" + alice, "", 405},
		{"GET", "/nope", "", 404},
	} {
		do(c.m, c.p, c.body, c.want)
	}

	do("GET", "/openapi.json", "", 200)

	for _, e := range adaptertest.Errs(recorder.Verify(d, rec)) {
		assert.NotContains(t, e, "but spec does not declare it", "drift")
	}
}

func TestMalformedBodyIsRejected(t *testing.T) {
	for _, body := range []string{`{"displayName":"changed"}{}`, `{"displayName":"changed"}null`, `{"displayName":"changed"} garbage`, ""} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/users", strings.NewReader(body))
		w := httptest.NewRecorder()
		_, ok := decode[User](w, r)
		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}
