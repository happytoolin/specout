package specout_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawStatusRangesAndDefaults(t *testing.T) {
	for _, raw := range []any{
		map[string]any{"201": map[string]any{"description": "created"}, "4XX": map[string]any{"description": "client error"}, "default": map[string]any{"description": "other"}},
		map[string]map[string]string{"201": {"description": "created"}, "4XX": {"description": "client error"}, "default": {"description": "other"}},
	} {
		d := specout.New(specout.Config{Title: "t", Version: "1", ErrorType: listItem{}, DefaultErrors: []int{500}})
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, string]{HandlerFunc: noop, Raw: map[string]any{"responses": raw}})
		doc := buildDoc(t, d)
		assert.Len(t, opOf(t, doc, "/x", "get")["responses"], 3)
		key := bridge.NewKey(http.MethodGet, "/x")
		assert.Equal(t, map[int]bool{201: true}, declaredStatuses(t, d)[key])
		allowed := specStatuses(t, d)[key]
		assert.True(t, allowed[0])
		assert.True(t, allowed[201])
		for status := 400; status < 500; status++ {
			assert.True(t, allowed[status])
		}
		assert.NotContains(t, allowed, 200)
		assert.NotContains(t, allowed, 500)
	}
}

func TestRawResponseMustBeAnObject(t *testing.T) {
	d := newGen()
	raw := map[string]any{"200": "ok"}
	assert.PanicsWithValue(t, "specout: GET /x: Raw.responses: 200 must be a Response object", func() {
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, string]{HandlerFunc: noop, Raw: map[string]any{"responses": raw}})
	})
}

func TestDefaultResponseWithCoverageStatusAllowsOtherCodes(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", okWith(specout.Response{Key: "default", Status: 404}))
	key := bridge.NewKey(http.MethodGet, "/x")
	assert.Equal(t, map[int]bool{204: true, 404: true}, declaredStatuses(t, d)[key], "a default response keeps the 204")
	assert.True(t, specStatuses(t, d)[key][0])
	assert.Contains(t, opOf(t, buildDoc(t, d), "/x", "get")["responses"], "default")
}

func TestInvalidResponseKeyStatusCombinationsFail(t *testing.T) {
	for _, response := range []specout.Response{
		{Key: "4XX", Status: 201},
		{Key: "default", Status: 700},
		{Key: "5XX", Status: -1},
	} {
		assert.Panics(t, func() {
			docOf(t, http.MethodGet, "/x", okWith(response))
		})
	}
}

func TestMisusedResponseFieldsFail(t *testing.T) {
	type errBody struct {
		Message string `json:"message"`
	}
	type post = specout.Handler[errBody, specout.NoContent]
	for _, tc := range []struct {
		name, want string
		h          specout.Handler[errBody, specout.NoContent]
	}{
		{
			"empty response media type", "specout: POST /x: response 200: ContentTypes has an empty media type",
			post{HandlerFunc: noop, Responses: []specout.Response{{Status: 200, Type: errBody{}, ContentTypes: []string{""}}}},
		},
		{"empty request media type", "specout: POST /x: Request: ContentTypes has an empty media type", post{HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{""}}}},
		{
			"empty header name", `specout: POST /x: response 204: a Header needs a Name`,
			post{HandlerFunc: noop, Responses: []specout.Response{{Status: 204, Headers: []specout.Header{{}}}}},
		},
		{
			"duplicate header name", `specout: POST /x: response 204: header "x-id" is repeated; header names ignore case`,
			post{HandlerFunc: noop, Responses: []specout.Response{{Status: 204, Headers: []specout.Header{{Name: "X-Id"}, {Name: "x-id"}}}}},
		},
		{
			"reflect.Type as a Type", "specout: POST /x: response 404: a Type field takes a value such as Problem{}, not a reflect.Type",
			post{HandlerFunc: noop, Responses: []specout.Response{{Status: 404, Type: reflect.TypeFor[errBody]()}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.PanicsWithValue(t, tc.want, func() { docOf(t, http.MethodPost, "/x", tc.h) })
		})
	}
}

type defaultsTask struct {
	ID string `json:"id"`
}

type defaultsProblem struct {
	Title string `json:"title"`
}

func responsesOf[Req, Res any](t *testing.T, cfg specout.Config, method string, h specout.Handler[Req, Res]) map[string]any {
	t.Helper()
	cfg.Title, cfg.Version = "t", "1"
	d := specout.New(cfg)
	d.Document(method, "/x", h)
	responses, ok := opOf(t, buildDoc(t, d), "/x", strings.ToLower(method))["responses"].(map[string]any)
	require.True(t, ok)
	return responses
}

func bodyRef(responses map[string]any, key string) string {
	resp, _ := responses[key].(map[string]any)
	content, _ := resp["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	schema, _ := media["schema"].(map[string]any)
	ref, _ := schema["$ref"].(string)
	return ref
}

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
			return responsesOf(t, specout.Config{}, http.MethodDelete, specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusNotFound}},
			})
		}, []string{"204", "404"}},
		"default keeps 200": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, specout.Handler[struct{}, defaultsTask]{
				HandlerFunc: noop, Responses: []specout.Response{{Key: "default"}},
			})
		}, []string{"200", "default"}},
		"304 keeps 200": {func() map[string]any {
			return responsesOf(t, specout.Config{}, http.MethodGet, specout.Handler[struct{}, defaultsTask]{
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
			return responsesOf(t, specout.Config{}, http.MethodGet, specout.Handler[struct{}, defaultsTask]{
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

func TestWebhookErrorsHaveNoErrorType(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ErrorType: defaultsProblem{}})
	d.Webhook(http.MethodPost, "task.done", specout.Handler[defaultsTask, specout.NoContent]{
		Responses: []specout.Response{{Status: http.StatusGone}},
	})
	doc := buildDoc(t, d)
	hook, _ := doc["webhooks"].(map[string]any)["task.done"].(map[string]any)
	responses, _ := hook["post"].(map[string]any)["responses"].(map[string]any)
	assert.ElementsMatch(t, []string{"204", "410"}, keys(responses))
	assert.NotContains(t, responses["410"], "content")
}

func TestPublicRoutesGetNoAuthErrors(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1", Auth: []specout.AuthScheme{specout.Bearer},
		ErrorType: defaultsProblem{}, DefaultErrors: []int{http.StatusUnauthorized, http.StatusForbidden, 500},
	})
	d.Document(http.MethodGet, "/health", specout.Handler[struct{}, defaultsTask]{HandlerFunc: noop, Public: true})
	d.Document(http.MethodGet, "/login", specout.Handler[struct{}, defaultsTask]{
		HandlerFunc: noop, Public: true, Responses: []specout.Response{{Status: http.StatusUnauthorized}},
	})
	d.Document(http.MethodGet, "/me", specout.Handler[struct{}, defaultsTask]{HandlerFunc: noop})
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

type exampleCreate struct {
	ID    string `path:"id"`
	Title string `json:"title"`
}

type exampleTask struct {
	ID string `json:"id"`
}

func TestExampleTypeMustMatchTheBody(t *testing.T) {
	cfg := specout.Config{Title: "t", Version: "1", ErrorType: defaultsProblem{}}
	route := func(h specout.Handler[exampleCreate, exampleTask]) func() {
		return func() {
			h.HandlerFunc = noop
			specout.New(cfg).Document(http.MethodPost, "/x/{id}", h)
		}
	}
	for name, tc := range map[string]struct {
		h    specout.Handler[exampleCreate, exampleTask]
		want string
	}{
		"request": {
			specout.Handler[exampleCreate, exampleTask]{Request: specout.Request{
				Examples: map[string]specout.Example{"a": {Value: exampleTask{}}},
			}},
			"POST /x/{id}: Request: example \"a\": the value is a specout_test.exampleTask, but the body is a " +
				"specout_test.exampleCreate; use a specout_test.exampleCreate value",
		},
		"named request": {
			specout.Handler[exampleCreate, exampleTask]{Request: specout.Request{
				Examples: map[string]specout.Example{"a": {Value: &exampleTask{}}},
			}},
			`Request: example "a": the value is a specout_test.exampleTask`,
		},
		"response": {
			specout.Handler[exampleCreate, exampleTask]{Responses: []specout.Response{
				{Status: http.StatusCreated, Examples: map[string]specout.Example{"a": {Value: defaultsProblem{}}}},
			}},
			"response 201: example \"a\": the value is a specout_test.defaultsProblem, but the body is a " +
				"specout_test.exampleTask; use a specout_test.exampleTask value",
		},
		"error": {
			specout.Handler[exampleCreate, exampleTask]{Responses: []specout.Response{
				{Status: http.StatusNotFound, Examples: map[string]specout.Example{"a": {Value: exampleTask{}}}},
			}},
			"response 404: example \"a\": the value is a specout_test.exampleTask, but the body is a specout_test.defaultsProblem",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, panicMessage(t, route(tc.h)), tc.want)
		})
	}

	for name, h := range map[string]specout.Handler[exampleCreate, exampleTask]{
		"Req value":     {Request: specout.Request{Examples: map[string]specout.Example{"a": {Value: exampleCreate{Title: "a"}}}}},
		"map":           {Request: specout.Request{Examples: map[string]specout.Example{"a": {Value: map[string]any{"title": "a"}}}}},
		"raw JSON":      {Request: specout.Request{Examples: map[string]specout.Example{"a": {Value: json.RawMessage(`{"title":"a"}`)}}}},
		"pointer":       {Responses: []specout.Response{{Status: http.StatusOK, Examples: map[string]specout.Example{"a": {Value: &exampleTask{}}}}}},
		"error body":    {Responses: []specout.Response{{Status: http.StatusNotFound, Examples: map[string]specout.Example{"a": {Value: defaultsProblem{}}}}}},
		"explicit Type": {Responses: []specout.Response{{Status: http.StatusConflict, Type: exampleTask{}, Examples: map[string]specout.Example{"a": {Value: exampleTask{}}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, route(h))
		})
	}
}

func TestExampleOfAListBodyIsAList(t *testing.T) {
	msg := panicMessage(t, func() {
		specout.New(specout.Config{Title: "t", Version: "1"}).Document(http.MethodGet, "/x", specout.Handler[struct{}, []exampleTask]{
			HandlerFunc: noop,
			Responses:   []specout.Response{{Status: http.StatusOK, Examples: map[string]specout.Example{"a": {Value: exampleTask{}}}}},
		})
	})
	assert.Contains(t, msg, "the value is a specout_test.exampleTask, but the body is a []specout_test.exampleTask")
}

type statusRes struct {
	OK bool `json:"ok"`
}

func TestRangeStatusCode(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/partial", specout.Handler[struct{}, statusRes]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		Responses: []specout.Response{
			{Status: 404, Key: "4XX", Raw: map[string]any{"description": "not found"}},
			{Status: 200, Omit: true},
		},
	})
	doc := buildDoc(t, d)
	responses := opOf(t, doc, "/partial", "get")["responses"].(map[string]any)
	assert.Equal(t, "not found", responses["4XX"].(map[string]any)["description"])
	assert.NotContains(t, responses, "200", "Omit must drop the Res default")
	key := bridge.Key{Method: "GET", Path: "/partial"}
	declared := declaredStatuses(t, d)
	assert.True(t, declared[key][404], "the code named beside the range must be required")
	assert.False(t, declared[key][200], "Omit must drop the Res default from coverage")
}

func TestNoBodyStatuses(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/gone", specout.Handler[struct{}, statusRes]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		Responses:   []specout.Response{{Status: 101}, {Status: 204}, {Status: 205}, {Status: 304}, {Key: "1XX"}},
	})
	responses := opOf(t, buildDoc(t, d), "/gone", "get")["responses"].(map[string]any)
	for _, code := range []string{"101", "204", "205", "304", "1XX"} {
		resp, ok := responses[code].(map[string]any)
		require.True(t, ok, "missing %s", code)
		assert.NotContains(t, resp, "content", "%s must carry no content", code)
		assert.NotEmpty(t, resp["description"], "%s needs a description", code)
	}
	assert.NotContains(t, responses, "200", "a declared 2xx replaces the Res default 200")
}

func TestHeadCarriesNoContent(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1", ErrorType: statusRes{}, DefaultErrors: []int{http.StatusInternalServerError},
	})
	d.Document(http.MethodHead, "/x", specout.Handler[struct{}, statusRes]{
		HandlerFunc: noop,
		Responses: []specout.Response{
			{Status: http.StatusNotFound, Headers: []specout.Header{{Name: "X-Reason"}}},
			{Status: http.StatusPartialContent, Type: specout.File{}, ContentTypes: []string{"application/pdf"}},
		},
	})
	doc := buildDoc(t, d)
	responses, _ := opOf(t, doc, "/x", "head")["responses"].(map[string]any)
	require.Len(t, responses, 3, "the declared 206 replaces the Res default 200")
	for code, want := range map[string]string{"404": "Not Found", "206": "Partial Content", "500": "Internal Server Error"} {
		resp, _ := responses[code].(map[string]any)
		assert.NotContains(t, resp, "content", code)
		assert.Equal(t, want, resp["description"], code)
	}
	assert.Contains(t, responses["404"], "headers")
	assert.NotContains(t, doc, "components", "a HEAD operation reflects no body type")
}

func TestBodylessContentPanics(t *testing.T) {
	for key, resp := range map[string]specout.Response{
		"204": {Status: http.StatusNoContent, Type: statusRes{}},
		"205": {Status: http.StatusResetContent, Type: statusRes{}},
		"304": {Status: http.StatusNotModified, ContentTypes: []string{"text/plain"}},
		"1XX": {Key: "1XX", ContentTypes: []string{"application/json"}},
	} {
		h := specout.Handler[struct{}, statusRes]{HandlerFunc: noop, Responses: []specout.Response{resp}}
		assert.PanicsWithValue(t,
			"specout: GET /x: response "+key+": this status has no body; remove Type and ContentTypes",
			func() { newGen().Document(http.MethodGet, "/x", h) }, key)
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, statusRes]{
		HandlerFunc: noop,
		Responses: []specout.Response{
			{Status: http.StatusNoContent, Type: struct{}{}},
			{Key: "2XX", Status: http.StatusNoContent},
		},
	})
	responses, _ := opOf(t, buildDoc(t, d), "/x", "get")["responses"].(map[string]any)
	assert.NotContains(t, responses["204"], "content")
	assert.Contains(t, responses["2XX"], "content", "a 2XX response describes bodies too")
}

func TestRequestContentTypesNeedABody(t *testing.T) {
	type onlyParams struct {
		ID string `path:"id"`
	}
	assert.PanicsWithValue(t, "specout: POST /x: Handler.Request describes a body, but Req has no body; add a field with no path, query, header or cookie tag, or remove Handler.Request", func() {
		newGen().Document(http.MethodPost, "/x", specout.Handler[struct{}, statusRes]{
			HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{"application/json"}},
		})
	})
	assert.PanicsWithValue(t, "specout: POST /x/{id}: Handler.Request describes a body, but Req has no body; add a field with no path, query, header or cookie tag, or remove Handler.Request", func() {
		newGen().Document(http.MethodPost, "/x/{id}", specout.Handler[onlyParams, statusRes]{
			HandlerFunc: noop, Request: specout.Request{ContentTypes: []string{"application/json"}},
		})
	})
}
