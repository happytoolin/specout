package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

// publishedGraph is the path -> method -> operationId map of the eight
// operations this example takes from the live document
// (graph.microsoft.com/v1.0, 11546 paths). Every deviation is listed in
// examples/README.md.
var publishedGraph = map[string]map[string]string{
	"/me":                           {"get": "me.user.GetUser"},
	"/users":                        {"get": "users.user.ListUser", "post": "users.user.CreateUser"},
	"/users/{user-id}":              {"get": "users.user.GetUser", "patch": "users.user.UpdateUser", "delete": "users.user.DeleteUser"},
	"/users/{user-id}/photo/$value": {"get": "users.GetPhotoContent"},
	"/users/{user-id}/sendMail":     {"post": "users.user.sendMail"},
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

// dig walks a nested JSON object, failing the test at the first missing key.
func dig(t *testing.T, v any, keys ...string) map[string]any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("no %q in %v", k, v)
		}
		v, ok = m[k]
		if !ok {
			t.Fatalf("missing %q", k)
		}
	}
	return v.(map[string]any)
}

func graphOp(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	return dig(t, doc, "paths", path, method)
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

// eq fails when a published field is not what the document says.
func eq(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// haveKeys fails when any published property is absent; absent fails when a
// published omission is present. Label names the context of the last.
func haveKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q in %v", k, m)
		}
	}
}

func absent(t *testing.T, label string, m map[string]any, key string) {
	t.Helper()
	if v, ok := m[key]; ok {
		t.Errorf("%s: %q is present (%v)", label, key, v)
	}
}

func TestSpecMatchesPublishedDocument(t *testing.T) {
	paths := dig(t, buildGraphSpec(t), "paths")
	eq(t, "paths", len(paths), len(publishedGraph))
	ids := 0
	for p, methods := range publishedGraph {
		item := dig(t, paths, p)
		eq(t, p+" operations", len(item), len(methods))
		for m, id := range methods {
			eq(t, p+" "+m+" operationId", dig(t, item, m)["operationId"], id)
			ids++
		}
	}
	eq(t, "operation count", ids, 8)
}

// TestSpecKeepsPublishedShapes pins the Graph shapes the example exists to
// prove: the shared OData error envelope keyed by code, a hyphenated path
// parameter name, header parameters, OData dollar-prefixed query parameters,
// a binary media response, and a request body split out of Req.
func TestSpecKeepsPublishedShapes(t *testing.T) {
	doc := buildGraphSpec(t)
	comps := dig(t, doc, "components")
	schemas := dig(t, comps, "schemas")

	// the published document declares no securitySchemes
	absent(t, "components", comps, "securitySchemes")

	// every operation carries the ODataError envelope under the document's own
	// range keys: 4XX and 5XX, described "error", not a fan-out per code
	for path, methods := range publishedGraph {
		for m := range methods {
			responses := dig(t, graphOp(t, doc, path, m), "responses")
			for _, code := range []string{"4XX", "5XX"} {
				resp := dig(t, responses, code)
				eq(t, m+" "+path+" "+code+" description", resp["description"], "error")
				eq(t, m+" "+path+" "+code+" schema",
					dig(t, resp, "content", "application/json", "schema")["$ref"], "#/components/schemas/ODataError")
			}
			// the successes the document ranges are keyed 2XX; the operations
			// that answer 204 keep the concrete code
			if _, ok2XX := responses["2XX"]; !ok2XX {
				if _, ok204 := responses["204"]; !ok204 {
					t.Errorf("%s %s: neither a 2XX nor a 204 response", m, path)
				}
			}
			absent(t, m+" "+path, responses, "404")
		}
	}

	// the envelope, including the hyphenated innerError property names
	haveKeys(t, dig(t, schemas, "InnerError", "properties"), "request-id", "client-request-id", "date")

	// InnerError is reached only through MainError's pointer field, so it is
	// nested-only: the pointer must still widen to [InnerError, null].
	// Regression — the pointer field kept invopop's bare $ref.
	arms, _ := dig(t, schemas, "MainError", "properties", "innerError")["oneOf"].([]any)
	if len(arms) != 2 || arms[1].(map[string]any)["type"] != "null" {
		t.Errorf("MainError.innerError = %v, want a $ref and a null arm", arms)
	}

	me := graphOp(t, doc, "/me", "get")
	list := graphOp(t, doc, "/users", "get")
	del := graphOp(t, doc, "/users/{user-id}", "delete")
	mail := graphOp(t, doc, "/users/{user-id}/sendMail", "post")
	photo := graphOp(t, doc, "/users/{user-id}/photo/$value", "get")
	userParam := paramNamed(t, graphOp(t, doc, "/users/{user-id}", "get"), "user-id", "path")
	mailSchema := dig(t, mail, "requestBody", "content", "application/json", "schema")
	bodyProps := dig(t, schemas, strings.TrimPrefix(mailSchema["$ref"].(string), "#/components/schemas/"), "properties")

	// the published facts, one line each
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
		eq(t, c.label, c.got, c.want)
	}

	// DELETE keeps a description-only 204; sendMail's body is its own component
	// under the published property names, without the path parameter
	absent(t, "DELETE 204", dig(t, del, "responses", "204"), "content")
	paramNamed(t, mail, "user-id", "path")
	haveKeys(t, bodyProps, "Message", "SaveToSentItems")
	absent(t, "sendMail body", bodyProps, "user-id")
	haveKeys(t, dig(t, schemas, "UserCollectionResponse", "properties"), "@odata.count", "@odata.nextLink", "value")
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
		// the declared 404 and 400 a handler can reach
		{"GET", "/users/nope", "", 404},
		{"POST", "/users/nope/sendMail", "{}", 404},
		{"GET", "/users/nope/photo/$value", "", 404},
		{"POST", "/users", "{bad", 400},
		// an undeclared method on a declared route: gorilla answers 405, which is
		// no operation, so the recorder must key nothing for it
		{"HEAD", "/users/" + alice, "", 405},
		// an unmatched path is not drift: the recorder keys nothing for it
		{"GET", "/nope", "", 404},
	} {
		do(c.m, c.p, c.body, c.want)
	}

	// the spec is served, and its own request is a skip
	do("GET", "/openapi.json", "", 200)

	ft := &captureT{}
	recorder.Verify(ft, d, rec)
	for _, e := range ft.errs {
		if strings.Contains(e, "but spec does not declare it") {
			t.Errorf("drift: %s", e)
		}
	}
}

// captureT collects failures so a test can assert on one drift category
// without the other failing it.
type captureT struct{ errs []string }

func (c *captureT) Helper()                   {}
func (c *captureT) Errorf(f string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(f, a...)) }
func (c *captureT) Fatalf(f string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(f, a...)) }
