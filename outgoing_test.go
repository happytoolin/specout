package specout_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func buildErr(d *specout.Generator) error { return d.WriteJSON(io.Discard) }

func authGen() *specout.Generator {
	return specout.New(specout.Config{
		Title: "t", Version: "1",
		Auth:      []specout.AuthScheme{specout.Bearer},
		ErrorType: problem{}, DefaultErrors: []int{http.StatusInternalServerError},
	})
}

// A webhook is described as a route is, but it is not one of your routes: it
// has no path, the auth and the errors of your API do not apply to it, and the
// route checks do not see it.
func TestWebhookDescribesARequestYourAPISends(t *testing.T) {
	d := authGen()
	d.Document(http.MethodGet, "/jobs", specout.Get[job]{HandlerFunc: noop})
	d.Webhook(http.MethodPost, "orderCreated", specout.Event[orderEvent]{
		Summary: "An order was created", Request: specout.Request{Example: orderEvent{OrderID: "a"}},
	})
	d.Webhook(http.MethodGet, "orderCreated", specout.Get[job]{Description: "The receiver proves that it owns the URL."})
	require.NoError(t, d.CheckRoutes([]specout.RouteKey{specout.NewRouteKey(http.MethodGet, "/jobs")}))

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
	done := specout.NewCallback("jobDone", http.MethodPost, url, specout.Event[jobDone]{Summary: "The job finished"})
	check := specout.NewCallback("jobDone", http.MethodHead, url, specout.Event[struct{}]{})
	failed := specout.NewCallback("jobFailed", http.MethodPost, "https://example.com/{$request.query.tenant}", specout.Event[problem]{})

	d := authGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[startJob, job]{HandlerFunc: noop}.WithCallbacks(done, check, failed))
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
		Callbacks:   []specout.Callback{specout.NewCallback("done", http.MethodPost, "{$request.body#/callbackUrl}", specout.Event[jobDone]{})},
	})
	d.Webhook(http.MethodPost, "done", specout.Event[jobDone]{})
	statuses, err := d.SpecStatuses()
	require.NoError(t, err)
	assert.Equal(t, map[specout.RouteKey]map[int]bool{
		specout.NewRouteKey(http.MethodPost, "/jobs"): {http.StatusOK: true},
	}, statuses)
}

func TestOperationIDsAreUniqueAcrossOutgoingRequests(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/jobs", specout.Get[job]{HandlerFunc: noop, OperationID: "same"})
	d.Webhook(http.MethodPost, "done", specout.Event[jobDone]{OperationID: "same"})
	require.ErrorContains(t, buildErr(d), `POST webhook done: duplicate operationId "same", also on GET /jobs`)

	d = newGen()
	d.Document(http.MethodPost, "/jobs", specout.Handler[startJob, job]{
		HandlerFunc: noop,
		Callbacks: []specout.Callback{
			specout.NewCallback("a", http.MethodPost, "u", specout.Event[jobDone]{OperationID: "same"}),
			specout.NewCallback("b", http.MethodPost, "u", specout.Event[jobDone]{OperationID: "same"}),
		},
	})
	assert.ErrorContains(t, buildErr(d), `duplicate operationId "same"`)
}

func TestBadOutgoingRequestsPanic(t *testing.T) {
	type pathed struct {
		ID string `path:"id"`
	}
	event := specout.Event[jobDone]{}
	route := func(callbacks ...specout.Callback) func() {
		return func() {
			newGen().Document(http.MethodPost, "/x", specout.Get[job]{HandlerFunc: noop, Callbacks: callbacks})
		}
	}
	webhook := func(fn func(d *specout.Generator)) func() { return func() { fn(newGen()) } }
	for want, call := range map[string]func(){
		"Webhook needs a name":       webhook(func(d *specout.Generator) { d.Webhook(http.MethodPost, "", event) }),
		`method "post" is not valid`: webhook(func(d *specout.Generator) { d.Webhook("post", "a", event) }),
		"remove HandlerFunc":         webhook(func(d *specout.Generator) { d.Webhook(http.MethodPost, "a", specout.Event[jobDone]{HandlerFunc: noop}) }),
		`remove the tag path:"id"`:   webhook(func(d *specout.Generator) { d.Webhook(http.MethodPost, "a", specout.Event[pathed]{}) }),
		"POST webhook a: duplicate webhook": webhook(func(d *specout.Generator) {
			d.Webhook(http.MethodPost, "a", event)
			d.Webhook(http.MethodPost, "a", event)
		}),
		`Security names scheme "x"`: webhook(func(d *specout.Generator) {
			d.Webhook(http.MethodPost, "a", specout.Event[jobDone]{Security: []specout.Security{{Scheme: "x"}}})
		}),
		"NewCallback needs a name":         func() { specout.NewCallback("", http.MethodPost, "u", event) },
		"needs a name and a url":           func() { specout.NewCallback("a", http.MethodPost, "", event) },
		"make it with NewCallback":         route(specout.Callback{}),
		`repeats POST callback "a"`:        route(specout.NewCallback("a", http.MethodPost, "u", event), specout.NewCallback("a", http.MethodPost, "u", event)),
		"Handler.Request describes a body": route(specout.NewCallback("a", http.MethodPost, "u", specout.Event[struct{}]{Request: specout.Request{Description: "x"}})),
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
	// the target can register after the link
	d.Document(http.MethodGet, "/jobs/{id}", specout.Get[job]{HandlerFunc: noop})

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
	d.Document(http.MethodPost, "/jobs", specout.Get[job]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{"status": {OperationID: "getJob"}}}},
	})
	assert.ErrorContains(t, buildErr(d), `POST /jobs: response 200: link "status": no operation has operationId "getJob"`)
}

func TestBadLinksPanic(t *testing.T) {
	link := func(l specout.Link) func() {
		return func() {
			newGen().Document(http.MethodGet, "/x", specout.Get[job]{
				HandlerFunc: noop,
				Responses:   []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{"a": l}}},
			})
		}
	}
	for want, call := range map[string]func(){
		"set one of OperationID": link(specout.Link{}),
		`link "a": set one of`:   link(specout.Link{OperationID: "x", OperationRef: "y"}),
		"cannot be encoded":      link(specout.Link{OperationID: "x", RequestBody: make(chan int)}),
		"needs a Default":        link(specout.Link{OperationID: "x", Server: &specout.Server{URL: "u", Variables: map[string]specout.ServerVariable{"a": {}}}}),
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
	}.WithRequest(specout.Request{Encoding: map[string]specout.Encoding{
		"avatar": {
			ContentType: "image/png",
			Headers:     []specout.Header{{Name: "X-Width", Type: 0, Required: true}},
		},
		"labels": {Style: "pipeDelimited", Explode: new(false), AllowReserved: true},
	}}))
	encoding := child(t, opOf(t, doc, "/upload", "post"), "requestBody", "content", "multipart/form-data", "encoding")
	assert.Equal(t, "image/png", child(t, encoding, "avatar")["contentType"])
	assert.Equal(t, true, child(t, encoding, "avatar", "headers", "X-Width")["required"])
	assert.Equal(t, map[string]any{"style": "pipeDelimited", "explode": false, "allowReserved": true}, encoding["labels"])
}

// A route with several media types gets the encoding on its form types only.
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
		`"tags": the request body has no such field, only avatar, labels`: route(map[string]specout.Encoding{"tags": {}}),
		`"hidden": the request body has no such field`:                    route(map[string]specout.Encoding{"hidden": {}}),
		`Style "matrix" is not valid`:                                     route(map[string]specout.Encoding{"labels": {Style: "matrix"}}),
		"a Header needs a Name":                                           route(map[string]specout.Encoding{"avatar": {Headers: []specout.Header{{}}}}),
		"the request body is not a form; set Request.ContentTypes": func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[job, job]{
				HandlerFunc: noop, Request: specout.Request{Encoding: map[string]specout.Encoding{"id": {}}},
			})
		},
		"Req has no body": func() {
			newGen().Document(http.MethodPost, "/x", specout.Get[job]{
				HandlerFunc: noop, Request: specout.Request{Encoding: map[string]specout.Encoding{"id": {}}},
			})
		},
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func TestOperationServers(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop}.WithServers(
		specout.Server{URL: "https://{region}.example.com", Variables: map[string]specout.ServerVariable{"region": {Default: "eu"}}},
	))
	assert.Equal(t, []any{map[string]any{
		"url": "https://{region}.example.com", "variables": map[string]any{"region": map[string]any{"default": "eu"}},
	}}, opOf(t, doc, "/x", "get")["servers"])

	assert.Contains(t, panicMessage(t, func() {
		newGen().Document(http.MethodGet, "/x", specout.Get[job]{
			HandlerFunc: noop, Servers: []specout.Server{{URL: "u", Variables: map[string]specout.ServerVariable{"a": {}}}},
		})
	}), "needs a Default")
}

// A field with a typed form has one way to write it, and the panic names it.
func TestRawNamesTheTypedForm(t *testing.T) {
	config := func(key string) func() {
		return func() { specout.New(specout.Config{Title: "t", Version: "1", Raw: map[string]any{key: 1}}) }
	}
	operation := func(key string) func() {
		return func() {
			newGen().Document(http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop, Raw: map[string]any{key: 1}})
		}
	}
	response := func(key string) func() {
		return func() {
			newGen().Document(http.MethodGet, "/x", specout.Get[job]{
				HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusOK, Raw: map[string]any{key: 1}}},
			})
		}
	}
	for want, call := range map[string]func(){
		"use Config.JSONSchemaDialect":          config("jsonSchemaDialect"),
		"use Generator.Webhook":                 config("webhooks"),
		"use Handler.Callbacks":                 operation("callbacks"),
		"use Config.Servers or Handler.Servers": operation("servers"),
		"use Response.Links":                    response("links"),
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func TestJSONSchemaDialect(t *testing.T) {
	const dialect = "https://spec.openapis.org/oas/3.1/dialect/base"
	d := specout.New(specout.Config{Title: "t", Version: "1", JSONSchemaDialect: dialect})
	assert.Equal(t, dialect, buildDoc(t, d)["jsonSchemaDialect"])
}

// The README examples, so that they keep compiling and keep their meaning.
func TestReadmeOutgoingExamples(t *testing.T) {
	doc := newGen()
	doc.Webhook(http.MethodPost, "orderCreated", specout.Event[orderEvent]{
		Summary: "An order was created",
	})
	doc.Webhook(http.MethodPost, "orderPaid", specout.Event[orderEvent]{
		Responses: []specout.Response{{Key: "2XX", Description: "Any success status"}},
	})
	jobFinished := specout.NewCallback(
		"jobDone", http.MethodPost, "{$request.body#/callbackUrl}",
		specout.Event[jobDone]{Summary: "The job finished"},
	)
	routes := specout.Std(doc, http.NewServeMux())
	routes.Post("/jobs", specout.Handler[startJob, job]{
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
	routes.Get("/jobs/{id}", specout.Get[job]{HandlerFunc: noop, OperationID: "getJob"})

	built := buildDoc(t, doc)
	assert.Equal(t, map[string]any{"2XX": map[string]any{"description": "Any success status"}},
		child(t, built, "webhooks", "orderPaid", "post")["responses"])
	assert.Contains(t, child(t, opOf(t, built, "/jobs", "post"), "callbacks"), "jobDone")
	assert.Contains(t, child(t, opOf(t, built, "/jobs", "post"), "responses", "200", "links"), "status")
}
