package specout_test

import (
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A build returns every problem it finds, so one run shows all the fixes.
func TestBuildReportsEveryProblem(t *testing.T) {
	type stray struct {
		ID string `path:"idd"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/a/{id}", specout.Handler[stray, job]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/b/{id}/c/{id}", specout.Get[job]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/one", specout.Get[job]{HandlerFunc: noop, OperationID: "same"})
	d.Document(http.MethodGet, "/two", specout.Get[job]{HandlerFunc: noop, OperationID: "same"})
	d.Document(http.MethodGet, "/three", specout.Get[job]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{"next": {OperationID: "sam"}}}},
	})
	err := d.Build()
	require.Error(t, err)
	for _, want := range []string{
		`specout: GET /a/{id}: Req has a field tagged path:"idd", but the pattern has no {idd}; did you mean "id"?`,
		`specout: GET /b/{id}/c/{id}: path parameter "id" appears twice`,
		`GET /two: duplicate operationId "same", also on GET /one`,
		`GET /three: response 200: link "next": no operation has operationId "sam"; ` +
			`set OperationID on the target route; did you mean "same"?`,
	} {
		require.ErrorContains(t, err, want)
	}
}

func TestBuildFreezesAndCanRunTwice(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop})
	require.NoError(t, d.Build())
	require.NoError(t, d.Build())
	assert.Contains(t, panicMessage(t, func() {
		d.Document(http.MethodGet, "/y", specout.Get[job]{HandlerFunc: noop})
	}), "cannot register after the spec was built")
}

// A panic deep in the schema code names the route, which the code there does
// not know.
func TestBuildPanicNamesTheRoute(t *testing.T) {
	type bad struct {
		Ch chan int `json:"ch"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Get[bad]{HandlerFunc: noop})
	assert.Equal(t, `specout: GET /x: JSON cannot encode chan int; tag its field json:"-"`,
		panicMessage(t, func() { _ = d.Build() }))

	d = newGen()
	d.Webhook(http.MethodPost, "w", specout.Event[bad]{})
	assert.Contains(t, panicMessage(t, func() { _ = d.Build() }), "specout: POST webhook w: JSON cannot encode chan int")
}

// The tag parser ignores a keyword that it does not know, so a typo would
// remove a rule from the document with no message.
func TestUnknownSchemaKeywordsPanic(t *testing.T) {
	type typo struct {
		Name string `json:"name" jsonschema:"minLenght=1"`
	}
	type comma struct {
		Name string `json:"name" jsonschema:"description=one, two"`
	}
	type param struct {
		Limit int `jsonschema:"maximun=10" query:"limit"`
	}
	type good struct {
		Name string `json:"name" jsonschema:"description=one\\, two,minLength=1,readonly"`
	}
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Get[typo]{HandlerFunc: noop})
	}), `field Name: jsonschema keyword "minLenght" is not known; did you mean "minLength"?`)
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Get[comma]{HandlerFunc: noop})
	}), `jsonschema keyword "two" is not known; it needs a value such as minLength=1, and a comma inside a value is written \\,`)
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[param, job]{HandlerFunc: noop})
	}), `GET /x: field Limit: jsonschema keyword "maximun" is not known; did you mean "maximum"?`)

	doc := docOf(t, http.MethodGet, "/x", specout.Get[good]{HandlerFunc: noop})
	name := child(t, doc, "components", "schemas", "good", "properties", "name")
	assert.Equal(t, "one, two", name["description"])
}

func TestSilentMistakesPanic(t *testing.T) {
	gen := func(c specout.Config) func() {
		return func() { specout.New(c) }
	}
	for want, call := range map[string]func(){
		"specout: Config: set Title and Version":              gen(specout.Config{}),
		"specout: Config.DefaultErrors: set Config.ErrorType": gen(specout.Config{Title: "t", Version: "1", DefaultErrors: []int{500}}),
		"specout: Config.Servers: a server needs a URL":       gen(specout.Config{Title: "t", Version: "1", Servers: []specout.Server{{}}}),
		`specout: GET /x: a Response needs a Status, or a Key`: func() {
			newGen().Document(http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop, Responses: []specout.Response{{Type: problem{}}}})
		},
		"specout: GET /x: ExternalDocs needs a URL": func() {
			newGen().Document(http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop, ExternalDocs: &specout.ExternalDocs{}})
		},
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func TestMessagesSuggestTheNearestName(t *testing.T) {
	auth := func(a specout.AuthScheme) func() {
		return func() { specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{a}}) }
	}
	for want, call := range map[string]func(){
		`method "post" is not valid; use GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS or TRACE; did you mean "POST"?`: func() {
			newGen().Document("post", "/x", specout.Get[job]{HandlerFunc: noop})
		},
		`Type "htp" is not valid; use http, apiKey, oauth2, openIdConnect, mutualTLS; did you mean "http"?`: auth(specout.AuthScheme{Name: "a", Type: "htp"}),
		`flow "clientCredential" is not valid`: auth(specout.OAuth2("a", map[string]specout.OAuth2Flow{"clientCredential": {}})),
		`Security names scheme "bearerAuh", but Config.Auth does not declare it; did you mean "bearerAuth"?`: func() {
			d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{specout.Bearer}})
			d.Document(http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop, Security: []specout.Security{{Scheme: "bearerAuh"}}})
		},
		`Raw key "summry" is not allowed here; start an extension with x-; did you mean "summary"?`: func() {
			newGen().Document(http.MethodGet, "/x", specout.Get[job]{HandlerFunc: noop, Raw: map[string]any{"summry": "x"}})
		},
		`"avtar": the request body has no such field, only avatar, labels; did you mean "avatar"?`: func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[uploadForm, job]{
				HandlerFunc: noop, Request: specout.Request{Encoding: map[string]specout.Encoding{"avtar": {}}},
			})
		},
		`pattern "GET GET /x" is not valid; pass Handle "METHOD /path", and pass Get, Post and the other verb methods only /path`: func() {
			specout.Std(newGen(), http.NewServeMux()).Get("GET /x", specout.Get[job]{HandlerFunc: noop})
		},
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

// File is the one rule for a binary body: a Req, a Res or a Response.Type.
func TestFileIsABinaryBody(t *testing.T) {
	binary := map[string]any{"type": "string", "format": "binary"}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[specout.File]{HandlerFunc: noop})
	media := child(t, opOf(t, doc, "/x", "get"), "responses", "200", "content", "application/octet-stream")
	assert.Equal(t, binary, media["schema"])

	doc = docOf(t, http.MethodGet, "/x", specout.Get[job]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Type: specout.File{}, ContentTypes: []string{"application/pdf", "image/png"}}},
	})
	content := child(t, opOf(t, doc, "/x", "get"), "responses", "200", "content")
	assert.Equal(t, binary, child(t, content, "application/pdf")["schema"])
	assert.Equal(t, binary, child(t, content, "image/png")["schema"])
	assert.NotContains(t, content, "application/json")
}

func TestAuthConstructorsAndRequire(t *testing.T) {
	oauth := specout.OAuth2("oauth", map[string]specout.OAuth2Flow{
		"clientCredentials": {TokenURL: coverageFlowURL},
	})
	d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{
		specout.Bearer, specout.Basic, oauth,
		specout.OpenIDConnect("oidc", "https://example.com/.well-known/openid-configuration"),
		specout.MutualTLS("mtls"),
	}})
	d.Document(http.MethodGet, "/x", specout.Get[job]{
		HandlerFunc: noop, Security: []specout.Security{oauth.Require("read"), specout.Bearer.Require()},
	})
	doc := buildDoc(t, d)
	assert.Equal(t, []any{
		map[string]any{"oauth": []any{"read"}},
		map[string]any{"bearerAuth": []any{}},
	}, opOf(t, doc, "/x", "get")["security"])
	schemes := child(t, doc, "components", "securitySchemes")
	assert.Equal(t, map[string]any{"type": "http", "scheme": "bearer"}, schemes["bearerAuth"])
	assert.Equal(t, map[string]any{"type": "mutualTLS"}, schemes["mtls"])
	assert.Equal(t, "openIdConnect", child(t, schemes, "oidc")["type"])
}
