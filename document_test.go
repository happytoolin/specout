package specout_test

import (
	json "encoding/json/v2"
	"fmt"
	"maps"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNestedRawMapsAreDeterministic(t *testing.T) {
	var first []byte
	for range 40 {
		d := newGen()
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, string]{
			HandlerFunc: noop,
			Raw: map[string]any{
				"x-meta": map[string]any{
					"c": 3, "a": 1, "b": 2,
					"nested": []any{map[string]int{"f": 6, "d": 4, "e": 5}},
				},
			},
			Responses: []specout.Response{{Status: 200, Raw: map[string]any{
				"x-response": map[string]int{"z": 9, "x": 7, "y": 8},
			}}},
		})
		spec, err := d.Build()
		require.NoError(t, err)
		if first == nil {
			first = spec
		}
		require.Equal(t, first, spec, "fresh generators must produce identical bytes")
	}
}

func TestFailedBuildCanBeRetried(t *testing.T) {
	d := newGen()
	d.Document(http.MethodPost, "/x", specout.Handler[nestedEnvelope, specout.NoContent]{HandlerFunc: noop})
	assert.Panics(t, func() { _, _ = d.Build() })
	d.Variant[schemaEmail]("email")
	d.Variant[schemaSlack]("slack")
	first, err := d.Build()
	require.NoError(t, err)
	second, err := d.Build()
	require.NoError(t, err)
	assert.Equal(t, first, second, "a failed build must not poison the next build")
}

func TestConcurrentExportsAreStable(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, listItem]{HandlerFunc: noop})
	var output [32][]byte
	var errors [32]error
	var workers sync.WaitGroup
	for i := range output {
		workers.Go(func() { output[i], errors[i] = d.Build() })
	}
	workers.Wait()
	for i := range output {
		require.NoError(t, errors[i])
		assert.Equal(t, output[0], output[i])
	}
}

func TestOperationsThatBreakTheDocument(t *testing.T) {
	type get = specout.Handler[struct{}, string]
	for _, tc := range []struct {
		name, want string
		h          specout.Handler[struct{}, string]
	}{
		{
			"every response omitted", "specout: GET /x: every response is omitted; declare at least one",
			get{HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusOK, Omit: true}}},
		},
		{
			"unknown operation Raw key", `specout: GET /x: Raw key "securitySchemes" is not allowed here; start an extension with x-`,
			get{HandlerFunc: noop, Raw: map[string]any{"securitySchemes": []any{}}},
		},
		{
			"unknown response Raw key", `specout: GET /x: response 200: Raw key "foo" is not allowed here; start an extension with x-`,
			get{HandlerFunc: noop, Responses: []specout.Response{{Status: http.StatusOK, Raw: map[string]any{"foo": 1}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.PanicsWithValue(t, tc.want, func() { newGen().Document(http.MethodGet, "/x", tc.h) })
		})
	}
}

func TestRawOperationIDMustBeUnique(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/a", specout.Handler[struct{}, string]{HandlerFunc: noop, Raw: map[string]any{"operationId": "getB"}})
	d.Document(http.MethodGet, "/b", specout.Handler[struct{}, string]{HandlerFunc: noop})
	wantBuildErr(t, d, `specout: GET /b: duplicate operationId "getB", also on GET /a`)
}

func TestOmittedRoutesDoNotReserveOperationIDs(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/files/{path...}", noBody{HandlerFunc: okBody, OperationID: "same"})
	d.Document(http.MethodGet, "/items", noBody{HandlerFunc: okBody, OperationID: "same"})
	assert.Equal(t, []string{"/items"}, keys(docPaths(t, d)))
}

func TestServerVariablesMatchTheURL(t *testing.T) {
	config := func(s specout.Server) func() {
		return func() { specout.New(specout.Config{Title: "t", Version: "1", Servers: []specout.Server{s}}) }
	}
	url := "https://{region}.example.com"
	assert.Contains(t, panicMessage(t, config(specout.Server{URL: url, Variables: map[string]specout.ServerVariable{
		"regoin": {Default: "eu"},
	}})), "the URL has {region}, but Variables does not declare it")
	assert.Contains(t, panicMessage(t, config(specout.Server{URL: url, Variables: map[string]specout.ServerVariable{
		"region": {Default: "eu"}, "regoin": {},
	}})), `variable "regoin" has no {regoin} in the URL; did you mean "region"?`)

	d := specout.New(specout.Config{Title: "t", Version: "1", Servers: []specout.Server{{
		URL: "https://api.example.com{basePath}", Variables: map[string]specout.ServerVariable{"basePath": {}},
	}}})
	d.Document(http.MethodGet, "/x", okGet)
	server := buildDoc(t, d)["servers"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{"basePath": map[string]any{"default": ""}}, server["variables"])
}

func TestSecurityScopesAndSchemeFieldsAreChecked(t *testing.T) {
	oauth := specout.OAuth2("oauth", map[string]specout.OAuth2Flow{
		"clientCredentials": {TokenURL: coverageFlowURL, Scopes: map[string]string{"read": "Read access"}},
	})
	oidc := specout.OpenIDConnect("oidc", "https://example.com/.well-known/openid-configuration")
	d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{oauth, oidc}})
	assert.Contains(t, panicMessage(t, func() {
		d.Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Security: []specout.Security{oauth.Require("raed")}})
	}), `Security asks for scope "raed" of scheme "oauth", but no flow of it declares that scope; did you mean "read"?`)
	d.Document(http.MethodGet, "/y", specout.Handler[struct{}, job]{HandlerFunc: noop, Security: []specout.Security{oidc.Require("admin")}})
	buildDoc(t, d)

	for want, a := range map[string]specout.AuthScheme{
		`Config.Auth "k": BearerFormat is for type http, and this scheme is apiKey; remove BearerFormat`: {
			Name: "k", Type: "apiKey", Key: "X-Key", In: "header", BearerFormat: "JWT",
		},
		`Config.Auth "m": Scheme is for type http, and this scheme is mutualTLS; remove Scheme`: {
			Name: "m", Type: "mutualTLS", Scheme: "basic",
		},
	} {
		assert.Contains(t, panicMessage(t, func() {
			specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{a}})
		}), want)
	}
}

func TestRawCannotReplaceATypedField(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{specout.Bearer}})
	for want, h := range map[string]specout.Handler[job, job]{
		`Raw key "security" replaces Security or Public; set one of them`: {
			Security: []specout.Security{specout.Bearer.Require()}, Raw: map[string]any{"security": []any{}},
		},
		`Raw key "requestBody" replaces Request; set one of them`: {
			Request: specout.Request{Description: "d"}, Raw: map[string]any{"requestBody": map[string]any{}},
		},
		`Raw key "summary" replaces Summary; set one of them`: {Summary: "s", Raw: map[string]any{"summary": "t"}},
		`response 200: Raw key "content" replaces ContentTypes or Examples; set one of them`: {
			Responses: []specout.Response{{
				Status: http.StatusOK, Examples: map[string]specout.Example{"a": {Value: job{ID: "1"}}}, Raw: map[string]any{"content": map[string]any{}},
			}},
		},
	} {
		h.HandlerFunc = noop
		assert.Contains(t, panicMessage(t, func() { d.Document(http.MethodPost, "/x", h) }), want)
	}
}

func TestBuildReportsEachProblemOnce(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop})
	_, err := d.Build()
	require.ErrorContains(t, err, "GET /x: duplicate route")
	assert.NotContains(t, err.Error(), "duplicate operationId")
}

func TestOAuth2(t *testing.T) {
	require.Panics(t, func() {
		specout.New(specout.Config{
			Title: "t", Version: "1",
			Auth: []specout.AuthScheme{specout.OAuth2("x", map[string]specout.OAuth2Flow{"implicit": {}})},
		})
	})
}

func TestExternalDocsNeedsURL(t *testing.T) {
	require.Panics(t, func() {
		specout.New(specout.Config{
			Title: "t", Version: "1",
			ExternalDocs: &specout.ExternalDocs{Description: "no link"},
		})
	})
}

func TestConfigThatBreaksTheDocument(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		cfg        specout.Config
	}{
		{"license without name", "specout: Config.License: set Name; OpenAPI requires it", specout.Config{License: &specout.License{URL: "https://x.example"}}},
		{"tag without a docs URL", "specout: Config.Tags \"a\": ExternalDocs needs a URL", specout.Config{Tags: []specout.Tag{{Name: "a", ExternalDocs: &specout.ExternalDocs{}}}}},
		{
			"reflect.Type error envelope", "specout: Config.ErrorType: a Type field takes a value such as Problem{}, not a reflect.Type",
			specout.Config{ErrorType: reflect.TypeFor[statusRes](), DefaultErrors: []int{http.StatusInternalServerError}},
		},
		{
			"error status out of range", "specout: Config.DefaultErrors: status 600 is not valid; use 100-599, or 0 for the default response",
			specout.Config{ErrorType: statusRes{}, DefaultErrors: []int{600}},
		},
		{
			"error status with no body", "specout: Config.DefaultErrors: status 204 has no body, so it cannot carry ErrorType; remove it",
			specout.Config{ErrorType: statusRes{}, DefaultErrors: []int{http.StatusNoContent}},
		},
		{"tag without name", `specout: Config.Tags: tag name "" is empty or repeated; give each tag one name`, specout.Config{Tags: []specout.Tag{{Description: "d"}}}},
		{"duplicate tag", `specout: Config.Tags: tag name "a" is empty or repeated; give each tag one name`, specout.Config{Tags: []specout.Tag{{Name: "a"}, {Name: "a"}}}},
		{
			"auth name with a space", `specout: Config.Auth: scheme name "bearer auth" must contain only ASCII letters, digits, dots, hyphens or underscores`,
			specout.Config{Auth: []specout.AuthScheme{{Name: "bearer auth", Type: "http", Scheme: "bearer"}}},
		},
		{
			"duplicate auth name", "specout: Config.Auth \"bearerAuth\": the name is repeated; give each scheme one name",
			specout.Config{Auth: []specout.AuthScheme{specout.Bearer, specout.APIKey("bearerAuth", "k", "header")}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Title, tc.cfg.Version = "t", "1"
			assert.PanicsWithValue(t, tc.want, func() { specout.New(tc.cfg) })
		})
	}
}

type docReq struct {
	Session string `cookie:"session"`
	Trace   string `header:"X-Trace-Id"`
	Body    string `json:"body"`
}

type docRes struct {
	OK bool `json:"ok"`
}

func TestDocFields(t *testing.T) {
	d := specout.New(specout.Config{
		Title:   "t",
		Version: "1",
		Auth: []specout.AuthScheme{
			specout.Bearer,
			specout.APIKey("apiKey", "X-API-Key", "header"),
		},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example"},
	})
	r := http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("POST", "/things", specout.Handler[docReq, docRes]{
		HandlerFunc: func(http.ResponseWriter, *http.Request) {},
		Summary:     "s",
		Description: "longer description",
	})
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/ping", specout.Handler[struct{}, docRes]{
		HandlerFunc: func(http.ResponseWriter, *http.Request) {},
		Public:      true,
	})
	doc := buildDoc(t, d)
	post := opOf(t, doc, "/things", "post")
	params := paramsOf(t, post)
	assert.Equal(t, "cookie", params["session"]["in"], "session param")
	assert.Equal(t, "header", params["X-Trace-Id"]["in"], "X-Trace-Id param")
	assert.Equal(t, "longer description", post["description"], "description missing")
	assert.Equal(t, "postThings", post["operationId"], "operationId")
	assert.NotContains(t, post, "security", "non-public route inherits top-level security")

	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	bearer := schemes["bearerAuth"].(map[string]any)
	assert.Equal(t, "http", bearer["type"], "bearer type")
	assert.Equal(t, "bearer", bearer["scheme"], "bearer scheme")
	apikey := schemes["apiKey"].(map[string]any)
	assert.Equal(t, "apiKey", apikey["type"], "apiKey type")
	assert.Equal(t, "X-API-Key", apikey["name"], "apiKey name")
	assert.Equal(t, "header", apikey["in"], "apiKey in")
	sec := doc["security"].([]any)
	assert.Len(t, sec, 2, "security alternatives")

	ping := opOf(t, doc, "/ping", "get")
	assert.Empty(t, ping["security"], "public route security")

	assert.Equal(t, "https://x.example", doc["externalDocs"].(map[string]any)["url"], "externalDocs")
}

func TestOperationIDOverride(t *testing.T) {
	d, r := newGen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/users/{id}/profile", specout.Handler[struct{}, struct{ V int }]{
		HandlerFunc: noop,
		OperationID: "getUserProfile",
	})
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/users/{id}/settings", specout.Handler[struct{}, struct{ V int }]{
		HandlerFunc: noop,
	})
	doc := buildDoc(t, d)
	get := func(p string) string { return opOf(t, doc, p, "get")["operationId"].(string) }
	assert.Equal(t, "getUserProfile", get("/users/{id}/profile"), "override ignored")
	assert.Equal(t, "getUsersIdSettings", get("/users/{id}/settings"), "derived changed")
}

func TestDerivedOperationIDHasNoBraces(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/files/{name}.{ext}", specout.Handler[struct{}, string]{HandlerFunc: noop})
	assert.Equal(t, "getFilesName.ext", opOf(t, doc, "/files/{name}.{ext}", "get")["operationId"])
}

func TestResponseDescriptionsAreNeverEmpty(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1", ErrorType: struct{ M string }{}, DefaultErrors: []int{0}})
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, string]{HandlerFunc: noop, Responses: []specout.Response{{Status: 299}}})
	responses := opOf(t, buildDoc(t, d), "/x", "get")["responses"].(map[string]any)
	assert.Equal(t, "default response", responses["default"].(map[string]any)["description"])
	assert.Equal(t, "299 response", responses["299"].(map[string]any)["description"])
}

func TestBuildReportsEveryProblem(t *testing.T) {
	type stray struct {
		ID string `path:"idd"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/a/{id}", specout.Handler[stray, job]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/b/{id}/c/{id}", specout.Handler[struct{}, job]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/one", specout.Handler[struct{}, job]{HandlerFunc: noop, OperationID: "same"})
	d.Document(http.MethodGet, "/two", specout.Handler[struct{}, job]{HandlerFunc: noop, OperationID: "same"})
	d.Document(http.MethodGet, "/three", specout.Handler[struct{}, job]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{"next": {OperationID: "sam"}}}},
	})
	_, err := d.Build()
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
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop})
	require.NoError(t, buildErr(d))
	require.NoError(t, buildErr(d))
	assert.Contains(t, panicMessage(t, func() {
		d.Document(http.MethodGet, "/y", specout.Handler[struct{}, job]{HandlerFunc: noop})
	}), "cannot register after the spec was built")
}

func TestBuildPanicNamesTheRoute(t *testing.T) {
	type bad struct {
		Ch chan int `json:"ch"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, bad]{HandlerFunc: noop})
	assert.Equal(t, `specout: GET /x: JSON cannot encode chan int; tag its field json:"-"`,
		panicMessage(t, func() { _, _ = d.Build() }))

	d = newGen()
	d.Webhook(http.MethodPost, "w", specout.Handler[bad, specout.NoContent]{})
	assert.Contains(t, panicMessage(t, func() { _, _ = d.Build() }), "specout: POST webhook w: JSON cannot encode chan int")
}

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
		docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, typo]{HandlerFunc: noop})
	}), `field Name: jsonschema keyword "minLenght" is not known; did you mean "minLength"?`)
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, comma]{HandlerFunc: noop})
	}), `jsonschema keyword " two" starts or ends with a space; remove the space, and write a comma inside a value as \\,`)
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[param, job]{HandlerFunc: noop})
	}), `GET /x: field Limit: jsonschema keyword "maximun" is not known; did you mean "maximum"?`)

	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, good]{HandlerFunc: noop})
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
			newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Responses: []specout.Response{{Type: problem{}}}})
		},
		"specout: GET /x: ExternalDocs needs a URL": func() {
			newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, ExternalDocs: &specout.ExternalDocs{}})
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
			newGen().Document("post", "/x", specout.Handler[struct{}, job]{HandlerFunc: noop})
		},
		`Type "htp" is not valid; use http, apiKey, oauth2, openIdConnect, mutualTLS; did you mean "http"?`: auth(specout.AuthScheme{Name: "a", Type: "htp"}),
		`flow "clientCredential" is not valid`: auth(specout.OAuth2("a", map[string]specout.OAuth2Flow{"clientCredential": {}})),
		`Security names scheme "bearerAuh", but Config.Auth does not declare it; did you mean "bearerAuth"?`: func() {
			d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{specout.Bearer}})
			d.Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Security: []specout.Security{{Scheme: "bearerAuh"}}})
		},
		`Raw key "summry" is not allowed here; start an extension with x-; did you mean "summary"?`: func() {
			newGen().Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{HandlerFunc: noop, Raw: map[string]any{"summry": "x"}})
		},
		`"avtar": the multipart/form-data body has no such field, only avatar, labels; did you mean "avatar"?`: func() {
			newGen().Document(http.MethodPost, "/x", specout.Handler[uploadForm, job]{
				HandlerFunc: noop, Request: specout.Request{Encoding: map[string]specout.Encoding{"avtar": {}}},
			})
		},
		"specout: GET x: http.ServeMux rejects the pattern": func() {
			specout.Bind(newGen(), specoutstd.New(http.NewServeMux())).Handle("GET", "x", specout.Handler[struct{}, job]{HandlerFunc: noop})
		},
		"specout: GET /x: GET /x matches the same requests as GET /x; register each route once, or change one of the paths": func() {
			routes := specout.Bind(newGen(), specoutstd.New(http.NewServeMux()))
			routes.Handle("GET", "/x", specout.Handler[struct{}, job]{HandlerFunc: noop})
			routes.Handle("GET", "/x", specout.Handler[struct{}, job]{HandlerFunc: noop})
		},
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func TestFileIsABinaryBody(t *testing.T) {
	binary := map[string]any{"type": "string", "format": "binary"}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, specout.File]{HandlerFunc: noop})
	media := child(t, opOf(t, doc, "/x", "get"), "responses", "200", "content", "application/octet-stream")
	assert.Equal(t, binary, media["schema"])

	doc = docOf(t, http.MethodGet, "/x", specout.Handler[struct{}, job]{
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
		"clientCredentials": {TokenURL: coverageFlowURL, Scopes: map[string]string{"read": "Read access"}},
	})
	d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{
		specout.Bearer, specout.Basic, oauth,
		specout.OpenIDConnect("oidc", "https://example.com/.well-known/openid-configuration"),
		specout.MutualTLS("mtls"),
	}})
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, job]{
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

type (
	defaultNotInEnum struct {
		Lang string `jsonschema:"enum=en|es,default=de" query:"lang,omitempty"`
	}
	defaultTooShort struct {
		Code string `json:"code" jsonschema:"minLength=3,default=ab"`
	}
	defaultNotInteger struct {
		N int `json:"n" jsonschema:"default=2.5"`
	}
	exampleTooShort struct {
		S string `json:"s" jsonschema:"minLength=5,example=ab"`
	}
	badAnchor struct {
		A string `json:"a" jsonschema:"anchor=1bad anchor"`
	}
	badPattern struct {
		B string `json:"b" jsonschema:"pattern=(a"`
	}
	zeroMultipleOf struct {
		N int `json:"n" jsonschema:"multipleOf=0"`
	}
	repeatedEnum struct {
		E string `json:"e" jsonschema:"enum=x,enum=x"`
	}
	extrasExclusiveMinimum struct {
		E int `json:"e" jsonschema_extras:"exclusiveMinimum=true"`
	}
	extrasRequired struct {
		R string `json:"r" jsonschema_extras:"required=true"`
	}
	extrasNullable struct {
		N string `json:"n" jsonschema_extras:"nullable=true"`
	}
	authHeader struct {
		Auth string `header:"Authorization"`
	}
	contentTypeHeader struct {
		CT string `header:"content-type"`
	}
	deepObjectString struct {
		Q string `jsonschema:"style=deepObject" query:"q"`
	}
	spaceDelimitedInt struct {
		S int `jsonschema:"style=spaceDelimited" query:"s,omitempty"`
	}
	deepObjectNoExplode struct {
		F map[string]string `jsonschema:"style=deepObject,explode=false" query:"f"`
	}
	fileField struct {
		F specout.File `json:"f"`
	}
	plainBody struct {
		M string `json:"m"`
	}
	refToNothing struct {
		V any `json:"v" jsonschema:"oneof_ref=#/$defs/Missing"`
	}
	refByName struct {
		W any `json:"w" jsonschema:"anyof_ref=Missing"`
	}
)

func handle[Req any](method, path string, h specout.Handler[Req, specout.NoContent]) func(*specout.Generator) {
	return func(d *specout.Generator) {
		h.HandlerFunc = noop
		d.Document(method, path, h)
	}
}

type panicCase struct {
	cfg  *specout.Config
	run  func(*specout.Generator)
	want string
}

func wantPanics(t *testing.T, cases map[string]panicCase) {
	t.Helper()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			msg := fmt.Sprint(panicValue(func() {
				d := newGen()
				if tc.cfg != nil {
					d = specout.New(*tc.cfg)
				}
				if tc.run != nil {
					tc.run(d)
				} else {
					d.Document(http.MethodGet, "/a", okGet)
				}
				_ = buildErr(d)
			}))
			assert.Contains(t, msg, "specout: ")
			assert.Contains(t, msg, tc.want)
		})
	}
}

func TestSchemaTagValuesPanic(t *testing.T) {
	wantPanics(t, map[string]panicCase{
		"default not in enum": {
			run:  handle(http.MethodGet, "/a", specout.Handler[defaultNotInEnum, specout.NoContent]{}),
			want: `field Lang: default "de" is not in the enum ["en","es"]; fix the default= tag`,
		},
		"default too short": {
			run:  handle(http.MethodPost, "/a", specout.Handler[defaultTooShort, specout.NoContent]{}),
			want: `default "ab" is shorter than minLength 3`,
		},
		"default not integer": {
			run:  handle(http.MethodPost, "/a", specout.Handler[defaultNotInteger, specout.NoContent]{}),
			want: "default 2.5 is not of type integer",
		},
		"example too short": {
			run:  handle(http.MethodPost, "/a", specout.Handler[exampleTooShort, specout.NoContent]{}),
			want: `example "ab" is shorter than minLength 5; fix the example= tag`,
		},
		"anchor": {
			run:  handle(http.MethodPost, "/a", specout.Handler[badAnchor, specout.NoContent]{}),
			want: `anchor "1bad anchor" is not valid`,
		},
		"pattern": {
			run:  handle(http.MethodPost, "/a", specout.Handler[badPattern, specout.NoContent]{}),
			want: `pattern "(a" is not a regular expression`,
		},
		"multipleOf": {
			run:  handle(http.MethodPost, "/a", specout.Handler[zeroMultipleOf, specout.NoContent]{}),
			want: "multipleOf=0 is not valid; use a number above 0",
		},
		"repeated enum": {
			run:  handle(http.MethodPost, "/a", specout.Handler[repeatedEnum, specout.NoContent]{}),
			want: `enum value "x" is repeated`,
		},
		"extras exclusiveMinimum": {
			run:  handle(http.MethodPost, "/a", specout.Handler[extrasExclusiveMinimum, specout.NoContent]{}),
			want: `jsonschema_extras key "exclusiveMinimum" has a checked form`,
		},
		"extras required": {
			run:  handle(http.MethodPost, "/a", specout.Handler[extrasRequired, specout.NoContent]{}),
			want: `jsonschema_extras key "required" has a checked form`,
		},
		"extras nullable": {
			run:  handle(http.MethodPost, "/a", specout.Handler[extrasNullable, specout.NoContent]{}),
			want: "jsonschema_extras key nullable is an OpenAPI 3.0 keyword",
		},
	})
}

func TestRequestChecksPanic(t *testing.T) {
	wantPanics(t, map[string]panicCase{
		"Authorization header": {
			run:  handle(http.MethodGet, "/a", specout.Handler[authHeader, specout.NoContent]{}),
			want: `OpenAPI ignores a header parameter named "Authorization"; describe it with Config.Auth`,
		},
		"Content-Type header in any case": {
			run:  handle(http.MethodGet, "/a", specout.Handler[contentTypeHeader, specout.NoContent]{}),
			want: `OpenAPI ignores a header parameter named "content-type"`,
		},
		"deepObject on a string": {
			run:  handle(http.MethodGet, "/a", specout.Handler[deepObjectString, specout.NoContent]{}),
			want: "style deepObject does not fit a string",
		},
		"spaceDelimited on an int": {
			run:  handle(http.MethodGet, "/a", specout.Handler[spaceDelimitedInt, specout.NoContent]{}),
			want: "style spaceDelimited does not fit a int",
		},
		"deepObject without explode": {
			run:  handle(http.MethodGet, "/a", specout.Handler[deepObjectNoExplode, specout.NoContent]{}),
			want: "style deepObject needs explode=true",
		},
		"file list body": {
			run: func(d *specout.Generator) {
				d.Document(http.MethodPost, "/a", specout.Handler[[]specout.File, specout.NoContent]{HandlerFunc: noop})
			},
			want: "a JSON body cannot carry files",
		},
		"file field in JSON": {
			run: handle(http.MethodPost, "/a", specout.Handler[fileField, specout.NoContent]{
				Request: specout.Request{ContentTypes: []string{"application/json"}},
			}),
			want: "Request.ContentTypes: application/json cannot carry the file fields of the body",
		},
		"TRACE body": {
			run:  handle(http.MethodTrace, "/a", specout.Handler[plainBody, specout.NoContent]{}),
			want: "a TRACE request has no body",
		},
		"media type without subtype": {
			run: handle(http.MethodPost, "/a", specout.Handler[plainBody, specout.NoContent]{
				Request: specout.Request{ContentTypes: []string{"json"}},
			}),
			want: `media type "json" is not valid; write type/subtype`,
		},
		"repeated media type": {
			run: handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{Responses: []specout.Response{{
				Status: 200, Type: plainBody{}, ContentTypes: []string{"application/json", "Application/JSON"},
			}}}),
			want: `media type "Application/JSON" is repeated`,
		},
		"Content-Type response header": {
			run: handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{Responses: []specout.Response{{
				Status: 204, Headers: []specout.Header{{Name: "Content-Type"}},
			}}}),
			want: "OpenAPI ignores a header named Content-Type here",
		},
		"empty tag": {
			run:  handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{Tags: []string{"a", ""}}),
			want: "Tags has an empty tag",
		},
		"path with a query": {
			run:  handle(http.MethodGet, "/a?x=1", specout.Handler[struct{}, specout.NoContent]{}),
			want: `the path has the character '?'`,
		},
	})
}

func TestRawValueChecksPanic(t *testing.T) {
	wantPanics(t, map[string]panicCase{
		"Raw operationId": {
			run: handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{
				Raw: map[string]any{"operationId": ""},
			}),
			want: `Raw key "operationId" must be a string that is not empty`,
		},
		"Raw security": {
			run: handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{
				Raw: map[string]any{"security": []any{map[string]any{"nope": []any{}}}},
			}),
			want: `Raw key "security" names scheme "nope", but Config.Auth does not declare it`,
		},
		"Raw response without description": {
			run: handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{
				Raw: map[string]any{"responses": map[string]any{"200": map[string]any{}}},
			}),
			want: `Raw.responses 200: a Response object needs a description`,
		},
		"Raw content on HEAD": {
			run: handle(http.MethodHead, "/a", specout.Handler[struct{}, specout.NoContent]{
				Raw: map[string]any{"responses": map[string]any{
					"200": map[string]any{"description": "x", "content": map[string]any{"application/json": map[string]any{}}},
				}},
			}),
			want: "a HEAD response has no body",
		},
		"Raw content on 204": {
			run: handle(http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{Responses: []specout.Response{{
				Status: 204, Raw: map[string]any{"content": map[string]any{"application/json": map[string]any{}}},
			}}}),
			want: `response 204: this status has no body; remove the Raw key "content"`,
		},
	})
}

func TestConfigURLChecksPanic(t *testing.T) {
	wantPanics(t, map[string]panicCase{
		"terms of service URL": {
			cfg:  &specout.Config{Title: "t", Version: "1", TermsOfService: "see our site"},
			want: `TermsOfService "see our site" is not a URL`,
		},
		"contact URL": {
			cfg:  &specout.Config{Title: "t", Version: "1", Contact: &specout.Contact{URL: "not a url"}},
			want: `URL "not a url" is not a URL`,
		},
		"contact email": {
			cfg:  &specout.Config{Title: "t", Version: "1", Contact: &specout.Contact{Email: "nobody"}},
			want: `Email "nobody" is not an email address`,
		},
		"external docs URL": {
			cfg:  &specout.Config{Title: "t", Version: "1", ExternalDocs: &specout.ExternalDocs{URL: "docs please"}},
			want: `"docs please" is not a URL`,
		},
		"OpenID Connect URL": {
			cfg: &specout.Config{
				Title: "t", Version: "1",
				Auth: []specout.AuthScheme{specout.OpenIDConnect("oidc", "well known")},
			},
			want: `URL "well known" is not a URL`,
		},
	})
}

func TestConformanceChecksFailTheBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		run  func(*specout.Generator)
		want string
	}{
		"$ref to nothing": {
			run:  handle(http.MethodPost, "/a", specout.Handler[refToNothing, specout.NoContent]{}),
			want: `$ref "#/components/schemas/Missing" points to nothing in the document`,
		},
		"$ref by name": {
			run:  handle(http.MethodPost, "/a", specout.Handler[refByName, specout.NoContent]{}),
			want: `$ref "Missing" is a name, not a reference; write #/$defs/Missing`,
		},
		"Raw parameters without the path parameter": {
			run: handle(http.MethodGet, "/a/{id}", specout.Handler[struct{}, specout.NoContent]{
				Raw: map[string]any{"parameters": []any{}},
			}),
			want: `Raw.parameters has no path parameter "id"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := newGen()
			tc.run(d)
			wantBuildErr(t, d, tc.want)
		})
	}
}

type (
	listDefaultQuery struct {
		IDs []int `jsonschema:"default=1" query:"ids,omitempty"`
	}
	listDefaultBody struct {
		Flags []bool `json:"flags" jsonschema:"default=true"`
	}
	emptyWithJSONSchema struct{}
)

func (emptyWithJSONSchema) JSONSchema() *specout.Schema { return &specout.Schema{Type: "string"} }

func TestListDefaultIsAList(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/a", specout.Handler[listDefaultQuery, specout.NoContent]{HandlerFunc: noop})
	params := opOf(t, doc, "/a", "get")["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, []any{float64(1)}, child(t, params[0], "schema")["default"])

	doc = docOf(t, http.MethodPost, "/a", specout.Handler[listDefaultBody, specout.NoContent]{HandlerFunc: noop})
	assert.Equal(t, []any{true}, child(t, componentsSchema(t, doc, "listDefaultBody"), "properties", "flags")["default"])
}

func TestBodiesThatStay(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/a", specout.Handler[struct{}, emptyWithJSONSchema]{HandlerFunc: noop})
	assert.Contains(t, child(t, opOf(t, doc, "/a", "get"), "responses", "200", "content"), "application/json")
	assert.Equal(t, "string", componentsSchema(t, doc, "emptyWithJSONSchema")["type"])

	doc = docOf(t, http.MethodGet, "/a", specout.Handler[plainBody, specout.NoContent]{HandlerFunc: noop})
	assert.Contains(t, child(t, opOf(t, doc, "/a", "get"), "requestBody", "content"), "application/json")
}

func TestRepeatedTagsAreWrittenOnce(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: noop, Tags: []string{"a", "b", "a"},
	})
	assert.Equal(t, []any{"a", "b"}, opOf(t, doc, "/a", "get")["tags"])
}

const coverageTable = "skills/specout/references/openapi-coverage.md"

func schemaFields(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("testdata/openapi-3.1-schema.json")
	require.NoError(t, err)
	var schema struct {
		Properties map[string]any `json:"properties"`
		Defs       map[string]any `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(data, &schema))
	fields := make(map[string]bool)
	for name := range schema.Properties {
		fields["root."+name] = true
	}
	for object, def := range schema.Defs {
		if object != "schema" && object != "styles-for-form" {
			collectProperties(object, def, fields)
		}
	}
	return slices.Sorted(maps.Keys(fields))
}

func collectProperties(object string, node any, fields map[string]bool) {
	switch n := node.(type) {
	case map[string]any:
		if props, ok := n["properties"].(map[string]any); ok {
			for name := range props {
				fields[object+"."+name] = true
			}
		}
		for _, child := range n {
			collectProperties(object, child, fields)
		}
	case []any:
		for _, child := range n {
			collectProperties(object, child, fields)
		}
	}
}

var (
	coverageHeading = regexp.MustCompile("^## ([a-z-]+)$")
	coverageRow     = regexp.MustCompile("^\\| `([^`]+)` \\| (Yes|In place|No) \\|")
)

func tableFields(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(coverageTable)
	require.NoError(t, err)
	fields := make(map[string]string)
	object := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		if m := coverageHeading.FindStringSubmatch(line); m != nil {
			object = m[1]
		}
		if m := coverageRow.FindStringSubmatch(line); m != nil && object != "" {
			fields[object+"."+m[1]] = m[2]
		}
	}
	return fields
}

var coverageChildren = map[string]map[string]string{
	"root":            {"info": "info", "servers[]": "server", "tags[]": "tag", "paths{}": "path-item", "webhooks{}": "path-item", "components": "components", "externalDocs": "external-documentation"},
	"info":            {"contact": "contact", "license": "license"},
	"server":          {"variables{}": "server-variable"},
	"components":      {"securitySchemes{}": "security-scheme"},
	"security-scheme": {"flows": "oauth-flows"},
	"oauth-flows":     {"*": "oauth-flows"},
	"tag":             {"externalDocs": "external-documentation"},
	"path-item":       {"get": "operation", "put": "operation", "post": "operation", "delete": "operation", "options": "operation", "head": "operation", "patch": "operation", "trace": "operation"},
	"operation":       {"externalDocs": "external-documentation", "parameters[]": "parameter", "requestBody": "request-body", "responses": "responses"},
	"request-body":    {"content{}": "media-type"},
	"media-type":      {"encoding{}": "encoding", "examples{}": "example"},
	"responses":       {"default": "response", "200": "response", "201": "response", "204": "response", "404": "response"},
	"response":        {"headers{}": "header", "content{}": "media-type", "links{}": "link"},
}

var oauthFlowNames = strings.Fields("implicit password clientCredentials authorizationCode")

var aliased = map[string]string{"media-type.example": "examples.example", "media-type.examples": "examples.examples"}

func seenFields(object string, node map[string]any, seen map[string]bool) {
	children := coverageChildren[object]
	for key, value := range node {
		field := object + "." + key
		if name, ok := aliased[field]; ok {
			field = name
		}
		seen[field] = true
		walkChild(children, key, value, seen)
	}
}

func walkChild(children map[string]string, key string, value any, seen map[string]bool) {
	list, _ := value.([]any)
	object, _ := value.(map[string]any)
	for suffix, items := range map[string][]any{"": {value}, "[]": list, "{}": slices.Collect(maps.Values(object))} {
		child, ok := children[key+suffix]
		if !ok && suffix == "" {
			child, ok = children["*"]
		}
		if !ok {
			continue
		}
		for _, item := range items {
			if m, isObj := item.(map[string]any); isObj {
				seenFields(child, m, seen)
			}
		}
	}
}

type coverageRequest struct {
	ID     string `jsonschema:"description=The ID"      path:"id"`
	Old    string `jsonschema:"deprecated"              query:"old,omitempty"`
	Empty  string `jsonschema:"allowEmptyValue=true"    query:"empty,omitempty"`
	Path   string `jsonschema:"allowReserved=true"      query:"path,omitempty"`
	Filter string `jsonschema:"style=form,explode=true" query:"filter,omitempty"`
	Name   string `json:"name"`
}

type coverageItem struct {
	Name string `json:"name"`
}

type coverageUpload struct {
	File specout.File `json:"file"`
}

const coverageFlowURL = "https://example.com/flow"

func coverageFlows() map[string]specout.OAuth2Flow {
	flows := make(map[string]specout.OAuth2Flow, len(oauthFlowNames))
	for _, name := range oauthFlowNames {
		flows[name] = specout.OAuth2Flow{
			AuthorizationURL: coverageFlowURL, TokenURL: coverageFlowURL, RefreshURL: coverageFlowURL,
			Scopes: map[string]string{"read": "Read"},
		}
	}
	return flows
}

func coverageGenerator() *specout.Generator {
	d := specout.New(specout.Config{
		Title: "Coverage", Version: "1.0.0", Summary: "Every field", Description: "All of them.",
		TermsOfService: "https://example.com/terms",
		Contact:        &specout.Contact{Name: "Team", URL: "https://example.com", Email: "team@example.com"},
		License:        &specout.License{Name: "Apache 2.0", Identifier: "Apache-2.0"},
		Servers: []specout.Server{{
			URL: "https://{region}.example.com", Description: "Regional",
			Variables: map[string]specout.ServerVariable{
				"region": {Default: "eu", Enum: []string{"eu", "us"}, Description: "The region"},
			},
		}},
		Tags: []specout.Tag{{
			Name: "items", Description: "Items",
			ExternalDocs: &specout.ExternalDocs{URL: "https://example.com/items", Description: "Item guide"},
		}},
		ExternalDocs: &specout.ExternalDocs{URL: "https://example.com/guide"},
		Auth: []specout.AuthScheme{
			specout.Basic,
			{Name: "jwt", Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "A signed token"},
			specout.MutualTLS("mtls"),
			specout.APIKey("key", "X-API-Key", "header"),
			specout.OpenIDConnect("oidc", "https://example.com/.well-known/openid-configuration"),
			specout.OAuth2("oauth", coverageFlows()),
		},
		Raw: map[string]any{"x-owner": "team"},
	})
	d.Webhook(http.MethodPost, "itemAdded", specout.Handler[coverageItem, specout.NoContent]{Summary: "An item was added"})
	d.Document(http.MethodPut, "/items/{id}", specout.Handler[coverageRequest, coverageItem]{
		HandlerFunc: noop, Tags: []string{"items"}, Summary: "Update", Description: "Updates an item.",
		OperationID: "updateItem", Deprecated: true,
		ExternalDocs: &specout.ExternalDocs{URL: "https://example.com/update"},
		Security:     []specout.Security{specout.OAuth2("oauth", coverageFlows()).Require("read"), specout.APIKey("key", "X-API-Key", "header").Require()},
		Request: specout.Request{
			Description: "The new item", Optional: true,
			Examples: map[string]specout.Example{
				"short": {Summary: "Short", Description: "A short name", Value: coverageRequest{ID: "7", Name: "a"}},
				"file":  {ExternalValue: "https://example.com/item.json"},
			},
		},
		Responses: []specout.Response{
			{
				Status: http.StatusOK, Description: "The item", Examples: map[string]specout.Example{"a": {Value: coverageItem{Name: "a"}}},
				Headers: []specout.Header{{Name: "X-Old", Description: "Old", Required: true, Deprecated: true}},
				Links: map[string]specout.Link{
					"byID":  {OperationID: "updateItem", Parameters: map[string]string{"id": "$response.body#/name"}, RequestBody: "$request.body", Description: "Again"},
					"byRef": {OperationRef: "#/paths/~1items~1{id}/put"},
				},
			},
			{Key: "default", Type: coverageItem{}},
		},
		Servers:   []specout.Server{{URL: "https://example.com"}},
		Callbacks: []specout.Callback{specout.NewCallback(http.MethodPost, "done", "{$request.body#/url}", specout.Handler[coverageItem, specout.NoContent]{})},
	})
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace} {
		d.Document(method, "/items", specout.Handler[struct{}, coverageItem]{HandlerFunc: noop, Public: true})
	}
	d.Document(http.MethodPost, "/upload", specout.Handler[coverageUpload, coverageItem]{
		HandlerFunc: noop,
		Request: specout.Request{Encoding: map[string]specout.Encoding{"file": {
			ContentType: "image/png", Headers: []specout.Header{{Name: "X-Width"}}, Style: "form", Explode: new(true), AllowReserved: true,
		}}},
		Responses: []specout.Response{{Status: http.StatusOK, Examples: map[string]specout.Example{"one": {Value: coverageItem{Name: "a"}}}}},
	})
	d.Document(http.MethodPost, "/once", specout.Handler[coverageItem, coverageItem]{HandlerFunc: noop, Request: specout.Request{
		Examples: map[string]specout.Example{"a": {Value: coverageItem{Name: "a"}}},
	}})
	return d
}

func linkServerDoc(t *testing.T) map[string]any {
	t.Helper()
	d := newGen()
	d.Document(http.MethodGet, "/x", specout.Handler[struct{}, coverageItem]{
		HandlerFunc: noop,
		Responses: []specout.Response{{Status: http.StatusOK, Links: map[string]specout.Link{
			"next": {OperationID: "getX", Server: &specout.Server{URL: "https://example.com"}},
		}}},
	})
	spec, err := d.Build()
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(spec, &doc))
	return doc
}

func TestOpenAPICoverage(t *testing.T) {
	table := tableFields(t)
	want := schemaFields(t)
	assert.Equal(t, want, slices.Sorted(maps.Keys(table)), "the table must list every field of the official schema, and no other")

	seen := make(map[string]bool)
	seenFields("root", buildDoc(t, coverageGenerator()), seen)
	licensed := specout.New(specout.Config{
		Title: "t", Version: "1", License: &specout.License{Name: "MIT", URL: "https://opensource.org/license/mit"},
	})
	seenFields("root", buildDoc(t, licensed), seen)
	seenFields("root", linkServerDoc(t), seen)
	for _, field := range want {
		status := table[field]
		if status == "Yes" {
			assert.True(t, seen[field], "%s is marked Yes, but the test document does not have it", field)
		}
	}
}

func TestCoverageFieldsAreWritten(t *testing.T) {
	doc := buildDoc(t, coverageGenerator())
	put := opOf(t, doc, "/items/{id}", "put")

	assert.Equal(t, []any{
		map[string]any{"oauth": []any{"read"}},
		map[string]any{"key": []any{}},
	}, put["security"])

	body, ok := put["requestBody"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, body["required"])
	assert.Equal(t, "The new item", body["description"])

	responses, ok := put["responses"].(map[string]any)
	require.True(t, ok)
	ok200, ok := responses["200"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "The item", ok200["description"])
	assert.Equal(t, map[string]any{
		"description": "Old", "required": true, "deprecated": true, "schema": map[string]any{"type": "string"},
	}, ok200["headers"].(map[string]any)["X-Old"])
	media := ok200["content"].(map[string]any)["application/json"].(map[string]any)
	assert.Equal(t, map[string]any{"name": "a"}, child(t, media, "examples", "a")["value"])

	params, ok := put["parameters"].([]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{
		"name": "old", "in": "query", "required": false, "deprecated": true, "schema": map[string]any{"type": "string"},
	}, params[1])
	assert.Equal(t, map[string]any{
		"name": "empty", "in": "query", "required": false, "allowEmptyValue": true, "schema": map[string]any{"type": "string"},
	}, params[2])

	assert.NotContains(t, doc, "jsonSchemaDialect")
	assert.Equal(t, "team", doc["x-owner"])
}

func TestHeadDropsResponseExamples(t *testing.T) {
	doc := docOf(t, http.MethodHead, "/x", specout.Handler[struct{}, coverageItem]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Examples: map[string]specout.Example{"a": {Value: coverageItem{Name: "a"}}}}},
	})
	response := opOf(t, doc, "/x", "head")["responses"].(map[string]any)["200"].(map[string]any)
	assert.NotContains(t, response, "content")
}

func TestBadCoverageDataPanics(t *testing.T) {
	config := func(c specout.Config) func() {
		c.Title, c.Version = "t", "1"
		return func() { specout.New(c) }
	}
	route := func(h specout.Handler[coverageItem, coverageItem]) func() {
		return func() {
			h.HandlerFunc = noop
			d := specout.New(specout.Config{Title: "t", Version: "1", Auth: []specout.AuthScheme{specout.Bearer}})
			d.Document(http.MethodPost, "/x", h)
		}
	}
	for want, call := range map[string]func(){
		"sets both URL and Identifier":       config(specout.Config{License: &specout.License{Name: "a", URL: "u", Identifier: "i"}}),
		`variable "a" has no {a} in the URL`: config(specout.Config{Servers: []specout.Server{{URL: "u", Variables: map[string]specout.ServerVariable{"a": {}}}}}),
		"Enum does not hold":                 config(specout.Config{Servers: []specout.Server{{URL: "u{a}", Variables: map[string]specout.ServerVariable{"a": {Default: "x", Enum: []string{"y"}}}}}}),
		"http needs a Scheme":                config(specout.Config{Auth: []specout.AuthScheme{{Name: "a", Type: "http"}}}),
		"BearerFormat needs the scheme":      config(specout.Config{Auth: []specout.AuthScheme{{Name: "a", Type: "http", Scheme: "basic", BearerFormat: "JWT"}}}),
		`Raw key "paths"`:                    config(specout.Config{Raw: map[string]any{"paths": 1}}),
		"does not declare":                   route(specout.Handler[coverageItem, coverageItem]{Security: []specout.Security{{Scheme: "other"}}}),
		"both Public and Security":           route(specout.Handler[coverageItem, coverageItem]{Public: true, Security: []specout.Security{{Scheme: "bearerAuth"}}}),
		"both Value and ExternalValue":       route(specout.Handler[coverageItem, coverageItem]{Request: specout.Request{Examples: map[string]specout.Example{"a": {Value: 1, ExternalValue: "u"}}}}),
		"cannot be encoded":                  route(specout.Handler[coverageItem, coverageItem]{Request: specout.Request{Examples: map[string]specout.Example{"a": {Value: make(chan int)}}}}),
		"sets an example, but this response has no body": route(specout.Handler[coverageItem, coverageItem]{
			Responses: []specout.Response{{Status: http.StatusNoContent, Examples: map[string]specout.Example{"a": {Value: 1}}}},
		}),
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

func panicMessage(t *testing.T, call func()) string {
	t.Helper()
	var message string
	func() {
		defer func() { message, _ = recover().(string) }()
		call()
	}()
	require.NotEmpty(t, message, "call did not panic with a message")
	return message
}

func TestParameterFlagsPanicOutsideQuery(t *testing.T) {
	type header struct {
		Token string `header:"X-Token,omitempty" jsonschema:"allowEmptyValue=true"`
	}
	type word struct {
		Filter string `jsonschema:"allowReserved=maybe" query:"filter,omitempty"`
	}
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[header, coverageItem]{HandlerFunc: noop})
	}), "allowEmptyValue works only on a query parameter")
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[word, coverageItem]{HandlerFunc: noop})
	}), "allowReserved=maybe")
}
