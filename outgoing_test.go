package specout_test

import (
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallbackMessagesNameTheRoute(t *testing.T) {
	url := "{$request.body#/url}"
	bad := specout.NewCallback(http.MethodPost, "done", url, specout.Handler[job, specout.NoContent]{Security: []specout.Security{{Scheme: "nope"}}})
	assert.Contains(t, panicMessage(t, func() {
		newGen().Document(http.MethodPost, "/a", specout.Handler[struct{}, job]{HandlerFunc: noop, Callbacks: []specout.Callback{bad}})
	}), `specout: POST /a: POST callback done: Security names scheme "nope"`)

	type unencodable struct {
		Ch chan int `json:"ch"`
	}
	d := newGen()
	d.Document(http.MethodPost, "/a", specout.Handler[struct{}, job]{HandlerFunc: noop, Callbacks: []specout.Callback{
		specout.NewCallback(http.MethodPost, "done", url, specout.Handler[unencodable, specout.NoContent]{}),
	}})
	assert.Equal(t, `specout: POST /a: POST callback done: JSON cannot encode chan int; tag its field json:"-"`,
		panicMessage(t, func() { _, _ = d.Build() }))

	shared := specout.NewCallback(http.MethodPost, "done", url, specout.Handler[job, specout.NoContent]{OperationID: "onDone"})
	d = newGen()
	d.Document(http.MethodPost, "/a", specout.Handler[struct{}, job]{HandlerFunc: noop, Callbacks: []specout.Callback{shared}})
	d.Document(http.MethodPost, "/b", specout.Handler[struct{}, job]{HandlerFunc: noop, Callbacks: []specout.Callback{shared}})
	wantBuildErr(t, d, `specout: POST /b: POST callback done: duplicate operationId "onDone", also on POST /a: `+
		`POST callback done; a Callback on several routes cannot set OperationID`)
}

type orderEvent struct {
	Signature string `header:"X-Signature"`
	OrderID   string `json:"orderId"`
}

type startJob struct {
	CallbackURL string `json:"callbackUrl"`
}

type jobDone struct {
	JobID string `json:"jobId"`
}

type job struct {
	ID string `json:"id"`
}

type problem struct {
	Detail string `json:"detail"`
}

func child(t *testing.T, node any, keys ...string) map[string]any {
	t.Helper()
	for _, key := range keys {
		m, ok := node.(map[string]any)
		require.True(t, ok, "%s is not under an object", key)
		node, ok = m[key]
		require.True(t, ok, "no %s in %v", key, m)
	}
	m, ok := node.(map[string]any)
	require.True(t, ok)
	return m
}

func buildErr(d *specout.Generator) error {
	_, err := d.Build()
	return err
}

func authGen() *specout.Generator {
	return specout.New(specout.Config{
		Title: "t", Version: "1",
		Auth:      []specout.AuthScheme{specout.Bearer},
		ErrorType: problem{}, DefaultErrors: []int{http.StatusInternalServerError},
	})
}

func TestWebhookDescribesARequestYourAPISends(t *testing.T) {
	d := authGen()
	d.Document(http.MethodGet, "/jobs", specout.Handler[struct{}, job]{HandlerFunc: noop})
	d.Webhook(http.MethodPost, "orderCreated", specout.Handler[orderEvent, specout.NoContent]{
		Summary: "An order was created", Request: specout.Request{Examples: map[string]specout.Example{"a": {Value: orderEvent{OrderID: "a"}}}},
	})
	d.Webhook(http.MethodGet, "orderCreated", specout.Handler[struct{}, job]{Description: "The receiver proves that it owns the URL."})
	require.NoError(t, specout.Check(d, fakeRouter{routes: []specout.Route{{Method: http.MethodGet, Path: "/jobs"}}}))

	doc := buildDoc(t, d)
	assert.NotContains(t, doc["paths"], "orderCreated")
	post := child(t, doc, "webhooks", "orderCreated", "post")
	assert.Equal(t, "An order was created", post["summary"])
	assert.NotContains(t, post, "operationId")
	assert.Equal(t, []any{}, post["security"])
	assert.Equal(t, map[string]any{"204": map[string]any{"description": "No content"}}, post["responses"])
	assert.Equal(t, "X-Signature", post["parameters"].([]any)[0].(map[string]any)["name"])
	media := child(t, post, "requestBody", "content", "application/json")
	assert.Equal(t, "#/components/schemas/orderEventBody", media["schema"].(map[string]any)["$ref"])
	assert.Contains(t, child(t, doc, "components", "schemas"), "orderEventBody")
	assert.Contains(t, child(t, doc, "webhooks", "orderCreated"), "get")
}

func TestOutgoingKeepsItsOwnSecurityAndResponses(t *testing.T) {
	d := authGen()
	d.Webhook(http.MethodPost, "signed", specout.Handler[orderEvent, job]{
		Security:  []specout.Security{{Scheme: "bearerAuth"}},
		Responses: []specout.Response{{Key: "4XX", Type: problem{}}},
	})
	post := child(t, buildDoc(t, d), "webhooks", "signed", "post")
	assert.Equal(t, []any{map[string]any{"bearerAuth": []any{}}}, post["security"])
	responses := child(t, post, "responses")
	assert.Contains(t, responses, "200")
	assert.Contains(t, responses, "4XX")
	assert.NotContains(t, responses, "500")
}

func TestCallbacksGroupUnderNameAndURL(t *testing.T) {
	const url = "{$request.body#/callbackUrl}"
	done := specout.NewCallback(http.MethodPost, "jobDone", url, specout.Handler[jobDone, specout.NoContent]{Summary: "The job finished"})
	check := specout.NewCallback(http.MethodHead, "jobDone", url, specout.Handler[struct{}, specout.NoContent]{})
	failed := specout.NewCallback(http.MethodPost, "jobFailed", "https://example.com/{$request.query.tenant}", specout.Handler[problem, specout.NoContent]{})

	d := authGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[startJob, job]{HandlerFunc: noop, Callbacks: []specout.Callback{done, check, failed}})
	d.Document(http.MethodPut, "/jobs", specout.Handler[startJob, job]{HandlerFunc: noop, Callbacks: []specout.Callback{done}})

	doc := buildDoc(t, d)
	callbacks := child(t, opOf(t, doc, "/jobs", "post"), "callbacks")
	item := child(t, callbacks, "jobDone", url)
	assert.Equal(t, "The job finished", child(t, item, "post")["summary"])
	assert.Contains(t, item, "head")
	assert.Contains(t, child(t, callbacks, "jobFailed"), "https://example.com/{$request.query.tenant}")
	assert.Equal(t, []any{}, child(t, item, "post")["security"])
	assert.Contains(t, child(t, doc, "components", "schemas"), "jobDone")
	assert.Contains(t, child(t, opOf(t, doc, "/jobs", "put"), "callbacks"), "jobDone")
}

func TestRouteChecksIgnoreOutgoingRequests(t *testing.T) {
	d := newGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[startJob, job]{
		HandlerFunc: noop,
		Callbacks:   []specout.Callback{specout.NewCallback(http.MethodPost, "done", "{$request.body#/callbackUrl}", specout.Handler[jobDone, specout.NoContent]{})},
	})
	d.Webhook(http.MethodPost, "done", specout.Handler[jobDone, specout.NoContent]{})
	statuses, err := bridge.Statuses(d, true)
	require.NoError(t, err)
	assert.Equal(t, map[bridge.Key]map[int]bool{
		bridge.NewKey(http.MethodPost, "/jobs"): {http.StatusOK: true},
	}, statuses)
}

func TestOperationIDsAreUniqueAcrossOutgoingRequests(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/jobs", specout.Handler[struct{}, job]{HandlerFunc: noop, OperationID: "same"})
	d.Webhook(http.MethodPost, "done", specout.Handler[jobDone, specout.NoContent]{OperationID: "same"})
	require.ErrorContains(t, buildErr(d), `POST webhook done: duplicate operationId "same", also on GET /jobs`)

	d = newGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[startJob, job]{
		HandlerFunc: noop,
		Callbacks: []specout.Callback{
			specout.NewCallback(http.MethodPost, "a", "u", specout.Handler[jobDone, specout.NoContent]{OperationID: "same"}),
			specout.NewCallback(http.MethodPost, "b", "u", specout.Handler[jobDone, specout.NoContent]{OperationID: "same"}),
		},
	})
	assert.ErrorContains(t, buildErr(d), `duplicate operationId "same"`)
}

func TestBadOutgoingRequestsPanic(t *testing.T) {
	type pathed struct {
		ID string `path:"id"`
	}
	event := specout.Handler[jobDone, specout.NoContent]{}
	route := func(callbacks ...specout.Callback) func() {
		return func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Callbacks: callbacks})
		}
	}
	webhook := func(fn func(d *specout.Generator)) func() { return func() { fn(newGen()) } }
	for want, call := range map[string]func(){
		"Webhook needs a name":       webhook(func(d *specout.Generator) { d.Webhook(http.MethodPost, "", event) }),
		`method "post" is not valid`: webhook(func(d *specout.Generator) { d.Webhook("post", "a", event) }),
		"remove HandlerFunc": webhook(func(d *specout.Generator) {
			d.Webhook(http.MethodPost, "a", specout.Handler[jobDone, specout.NoContent]{HandlerFunc: noop})
		}),
		`remove the tag path:"id"`: webhook(func(d *specout.Generator) {
			d.Webhook(http.MethodPost, "a", specout.Handler[pathed, specout.NoContent]{})
		}),
		"POST webhook a: duplicate webhook": webhook(func(d *specout.Generator) {
			d.Webhook(http.MethodPost, "a", event)
			d.Webhook(http.MethodPost, "a", event)
		}),
		`Security names scheme "x"`: webhook(func(d *specout.Generator) {
			d.Webhook(http.MethodPost, "a", specout.Handler[jobDone, specout.NoContent]{Security: []specout.Security{{Scheme: "x"}}})
		}),
		"NewCallback needs a name":         func() { specout.NewCallback(http.MethodPost, "", "u", event) },
		"needs a name and a url":           func() { specout.NewCallback(http.MethodPost, "a", "", event) },
		"make it with NewCallback":         route(specout.Callback{}),
		`repeats POST callback "a"`:        route(specout.NewCallback(http.MethodPost, "a", "u", event), specout.NewCallback(http.MethodPost, "a", "u", event)),
		"Handler.Request describes a body": route(specout.NewCallback(http.MethodPost, "a", "u", specout.Handler[struct{}, specout.NoContent]{Request: specout.Request{Description: "x"}})),
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}

	d := newGen()
	require.NoError(t, buildErr(d))
	assert.Contains(t, panicMessage(t, func() { d.Webhook(http.MethodPost, "late", event) }), "after the spec was built")
}

func TestResponseLinks(t *testing.T) {
	d := newGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[startJob, job]{
		HandlerFunc: noop,
		Responses: []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{
			"status": {
				OperationID: "getJobsId", Description: "Read the job",
				Parameters:  map[string]string{"id": "$response.body#/id", "fresh": "true"},
				RequestBody: "$request.body",
			},
			"elsewhere": {OperationRef: "https://example.com/openapi.json#/paths/~1a/get"},
		}}},
	})
	d.Document(http.MethodGet, "/jobs/{id}", specout.Handler[struct{}, job]{HandlerFunc: noop})

	links := child(t, opOf(t, buildDoc(t, d), "/jobs", "post"), "responses", "200", "links")
	assert.Equal(t, map[string]any{
		"operationId": "getJobsId", "description": "Read the job",
		"parameters":  map[string]any{"id": "$response.body#/id", "fresh": "true"},
		"requestBody": "$request.body",
	}, links["status"])
	assert.Equal(t, map[string]any{"operationRef": "https://example.com/openapi.json#/paths/~1a/get"}, links["elsewhere"])
}

func TestLinkServer(t *testing.T) {
	link := child(t, opOf(t, linkServerDoc(t), "/x", "get"), "responses", "200", "links", "next")
	assert.Equal(t, map[string]any{"url": "https://example.com"}, link["server"])
}

func TestLinkTargetMustExist(t *testing.T) {
	d := newGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[struct{}, job]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{"status": {OperationID: "getJob"}}}},
	})
	assert.ErrorContains(t, buildErr(d), `POST /jobs: response 200: link "status": no operation has operationId "getJob"`)
}

func TestBadLinksPanic(t *testing.T) {
	link := func(l specout.Link) func() {
		return func() {
			newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{
				HandlerFunc: noop,
				Responses:   []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{"a": l}}},
			})
		}
	}
	for want, call := range map[string]func(){
		"set one of OperationID": link(specout.Link{}),
		`link "a": set one of`:   link(specout.Link{OperationID: "x", OperationRef: "y"}),
		"cannot be encoded":      link(specout.Link{OperationID: "x", RequestBody: make(chan int)}),
		"has no {a} in the URL":  link(specout.Link{OperationID: "x", Server: &specout.Server{URL: "u", Variables: map[string]specout.ServerVariable{"a": {}}}}),
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

type uploadForm struct {
	Avatar specout.File `json:"avatar"`
	Tags   []string     `form:"labels" json:"tags"`
	Hidden string       `form:"-"      json:"hidden"`
}

func TestRequestEncoding(t *testing.T) {
	doc := docOf(t, http.MethodPost, "/upload", specout.Handler[uploadForm, job]{
		HandlerFunc: noop,
		Request: specout.Request{Encoding: map[string]specout.Encoding{
			"avatar": {
				ContentType: "image/png",
				Headers:     []specout.Header{{Name: "X-Width", Type: 0, Required: true}},
			},
			"labels": {Style: "pipeDelimited", Explode: new(false), AllowReserved: true},
		}},
	})
	encoding := child(t, opOf(t, doc, "/upload", "post"), "requestBody", "content", "multipart/form-data", "encoding")
	assert.Equal(t, "image/png", child(t, encoding, "avatar")["contentType"])
	assert.Equal(t, true, child(t, encoding, "avatar", "headers", "X-Width")["required"])
	assert.Equal(t, map[string]any{"style": "pipeDelimited", "explode": false, "allowReserved": true}, encoding["labels"])
}

func TestRequestEncodingSkipsOtherMediaTypes(t *testing.T) {
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[job, job]{
		HandlerFunc: noop,
		Request: specout.Request{
			ContentTypes: []string{"application/json", "application/x-www-form-urlencoded"},
			Encoding:     map[string]specout.Encoding{"id": {Style: "form"}},
		},
	})
	content := child(t, opOf(t, doc, "/x", "post"), "requestBody", "content")
	assert.NotContains(t, child(t, content, "application/json"), "encoding")
	assert.Contains(t, child(t, content, "application/x-www-form-urlencoded"), "encoding")
}

func TestBadRequestEncodingPanics(t *testing.T) {
	route := func(encoding map[string]specout.Encoding) func() {
		return func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[uploadForm, job]{HandlerFunc: noop, Request: specout.Request{Encoding: encoding}})
		}
	}
	for want, call := range map[string]func(){
		`"tags": the multipart/form-data body has no such field, only avatar, labels`: route(map[string]specout.Encoding{"tags": {}}),
		`"hidden": the multipart/form-data body has no such field`:                    route(map[string]specout.Encoding{"hidden": {}}),
		`Style "matrix" is not valid`:                                                 route(map[string]specout.Encoding{"labels": {Style: "matrix"}}),
		"a Header needs a Name":                                                       route(map[string]specout.Encoding{"avatar": {Headers: []specout.Header{{}}}}),
		"the request body is not a form; set Request.ContentTypes": func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[job, job]{
				HandlerFunc: noop, Request: specout.Request{Encoding: map[string]specout.Encoding{"id": {}}},
			})
		},
		"Req has no body": func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[struct{}, job]{
				HandlerFunc: noop, Request: specout.Request{Encoding: map[string]specout.Encoding{"id": {}}},
			})
		},
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func TestOperationServers(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Servers: []specout.Server{
		{URL: "https://{region}.example.com", Variables: map[string]specout.ServerVariable{"region": {Default: "eu"}}},
	}})
	assert.Equal(t, []any{map[string]any{
		"url": "https://{region}.example.com", "variables": map[string]any{"region": map[string]any{"default": "eu"}},
	}}, opOf(t, doc, "/x", "get")["servers"])

	assert.Contains(t, panicMessage(t, func() {
		newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{
			HandlerFunc: noop, Servers: []specout.Server{{URL: "u", Variables: map[string]specout.ServerVariable{"a": {}}}},
		})
	}), `variable "a" has no {a} in the URL`)
}

func TestRawNamesTheTypedForm(t *testing.T) {
	config := func(key string) func() {
		return func() { specout.New(specout.Config{Title: "t", Version: "1", Raw: map[string]any{key: 1}}) }
	}
	operation := func(key string) func() {
		return func() {
			newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Raw: map[string]any{key: 1}})
		}
	}
	response := func(key string) func() {
		return func() {
			newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusOK, Raw: map[string]any{key: 1}}},
			})
		}
	}
	for want, call := range map[string]func(){
		"use Generator.Webhook":                 config("webhooks"),
		"use Handler.Callbacks":                 operation("callbacks"),
		"use Config.Servers or Handler.Servers": operation("servers"),
		"use Response.Links":                    response("links"),
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func TestReadmeOutgoingExamples(t *testing.T) {
	doc := newGen()
	doc.Webhook(http.MethodPost, "orderCreated", specout.Handler[orderEvent, specout.NoContent]{
		Summary: "An order was created",
	})
	doc.Webhook(http.MethodPost, "orderPaid", specout.Handler[orderEvent, specout.NoContent]{
		Responses: []specout.Response{{Key: "2XX", Description: "Any success status"}},
	})
	jobFinished := specout.NewCallback(
		http.MethodPost, "jobDone", "{$request.body#/callbackUrl}",
		specout.Handler[jobDone, specout.NoContent]{Summary: "The job finished"},
	)
	routes := specout.Bind(doc, specoutstd.New(http.NewServeMux()))
	routes.Handle("POST", "/jobs", specout.Handler[startJob, job]{
		HandlerFunc: noop,
		Callbacks:   []specout.Callback{jobFinished},
		Responses: []specout.Response{{
			Status: http.StatusOK,
			Links: map[string]specout.Link{
				"status": {
					OperationID: "getJob",
					Parameters:  map[string]string{"id": "$response.body#/id"},
				},
			},
		}},
	})
	routes.Handle("GET", "/jobs/{id}", specout.Handler[struct{}, job]{HandlerFunc: noop, OperationID: "getJob"})

	built := buildDoc(t, doc)
	assert.Equal(t, map[string]any{"2XX": map[string]any{"description": "Any success status"}},
		child(t, built, "webhooks", "orderPaid", "post")["responses"])
	assert.Contains(t, child(t, opOf(t, built, "/jobs", "post"), "callbacks"), "jobDone")
	assert.Contains(t, child(t, opOf(t, built, "/jobs", "post"), "responses", "200", "links"), "status")
}
