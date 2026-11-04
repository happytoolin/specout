package specout_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/happytoolin/specout"
)

type defaultsTask struct {
	ID string `json:"id"`
}

type defaultsProblem struct {
	Title string `json:"title"`
}

// responsesOf builds a document with cfg and one route, and returns the
// responses of the route.
func responsesOf[Req, Res any](t *testing.T, cfg specout.Config, method string, h specout.Handler[Req, Res]) map[string]any {
	t.Helper()
	cfg.Title, cfg.Version = "t", "1"
	d := specout.New(cfg)
	d.Document(method, "/x", h)
	responses, ok := opOf(t, buildDoc(t, d), "/x", strings.ToLower(method))["responses"].(map[string]any)
	require.True(t, ok)
	return responses
}

// bodyRef is the $ref of the JSON body of a response, or "" with no body.
func bodyRef(responses map[string]any, key string) string {
	resp, _ := responses[key].(map[string]any)
	content, _ := resp["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	schema, _ := media["schema"].(map[string]any)
	ref, _ := schema["$ref"].(string)
	return ref
}

// A declared success response replaces the inferred one. Any other response
// keeps it, so a 404 on a Delete keeps the 204, and a 304 keeps the 200.
func TestDeclaredSuccessReplacesTheInferredOne(t *testing.T) {
	cases := map[string]struct {
		h    func() map[string]any
		want []string
	}{
		"201 replaces 200": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodPost, specout.Handler[struct{}, defaultsTask]{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusCreated}, {Status: http.StatusBadRequest}},
			})
		}, []string{"201", "400"}},
		"404 keeps 204": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodDelete, specout.Delete{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusNotFound}},
			})
		}, []string{"204", "404"}},
		"default keeps 200": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, specout.Get[defaultsTask]{
				HandlerFunc: noop, Responses: []specout.Response{{Key: "default"}},
			})
		}, []string{"200", "default"}},
		"304 keeps 200": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, specout.Get[defaultsTask]{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusNotModified}},
			})
		}, []string{"200", "304"}},
		"302 replaces 204": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, noBody{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusFound}},
			})
		}, []string{"302"}},
		"101 replaces 204": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, noBody{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusSwitchingProtocols}},
			})
		}, []string{"101"}},
		"2XX replaces 200": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, specout.Get[defaultsTask]{
				HandlerFunc: noop, Responses: []specout.Response{{Key: "2XX"}},
			})
		}, []string{"2XX"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, keys(tc.h()))
		})
	}
}

// A response with no Type has the body of its class: Res on a 2xx, ErrorType
// on a 4xx, 5xx or default response, and no body on any other.
func TestResponseWithNoTypeTakesTheBodyOfItsClass(t *testing.T) {
	h := specout.Handler[struct{}, defaultsTask]{HandlerFunc: noop, Responses: []specout.Response{
		{Status: http.StatusCreated},
		{Status: http.StatusFound},
		{Status: http.StatusNotFound},
		{Key: "5XX"},
		{Key: "default"},
		{Status: http.StatusUnauthorized, Type: specout.NoContent{}},
	}}
	withErrors := responsesOf(t, specout.Config{ErrorType: defaultsProblem{}}, http.MethodPost, h)
	task, problem := "#/components/schemas/defaultsTask", "#/components/schemas/defaultsProblem"
	assert.Equal(t, task, bodyRef(withErrors, "201"))
	assert.Empty(t, bodyRef(withErrors, "302"), "a redirect has no body")
	for _, key := range []string{"404", "5XX", "default"} {
		assert.Equal(t, problem, bodyRef(withErrors, key), key)
	}
	assert.NotContains(t, withErrors["401"], "content", "Type: NoContent{} removes the body")

	plain := responsesOf(t, specout.Config{}, http.MethodPost, h)
	for _, key := range []string{"404", "5XX", "default"} {
		assert.NotContains(t, plain[key], "content", "with no ErrorType, %s has no body", key)
	}
}

// A webhook goes to another server, so ErrorType does not describe its errors.
func TestWebhookErrorsHaveNoErrorType(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ErrorType: defaultsProblem{}})
	d.Webhook(http.MethodPost, "task.done", specout.Event[defaultsTask]{
		Responses: []specout.Response{{Status: http.StatusGone}},
	})
	doc := buildDoc(t, d)
	hook, _ := doc["webhooks"].(map[string]any)["task.done"].(map[string]any)
	responses, _ := hook["post"].(map[string]any)["responses"].(map[string]any)
	assert.ElementsMatch(t, []string{"204", "410"}, keys(responses))
	assert.NotContains(t, responses["410"], "content")
}

// A Public route needs no credentials, so DefaultErrors adds no 401 or 403 to
// it. A route that declares them keeps them.
func TestPublicRoutesGetNoAuthErrors(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1", Auth: []specout.AuthScheme{specout.Bearer},
		ErrorType: defaultsProblem{}, DefaultErrors: []int{http.StatusUnauthorized, http.StatusForbidden, 500},
	})
	d.Document(http.MethodGet, "/health", specout.Get[defaultsTask]{HandlerFunc: noop, Public: true})
	d.Document(http.MethodGet, "/login", specout.Get[defaultsTask]{
		HandlerFunc: noop, Public: true, Responses: []specout.Response{{Status: http.StatusUnauthorized}},
	})
	d.Document(http.MethodGet, "/me", specout.Get[defaultsTask]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	for path, want := range map[string][]string{
		"/health": {"200", "500"},
		"/login":  {"200", "401", "500"},
		"/me":     {"200", "401", "403", "500"},
	} {
		responses, _ := opOf(t, doc, path, "get")["responses"].(map[string]any)
		assert.ElementsMatch(t, want, keys(responses), path)
	}
}
