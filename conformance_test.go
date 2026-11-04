package specout_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each value below would make a document that breaks OpenAPI 3.1 or JSON
// Schema 2020-12. specout must stop with a fix, not write the document.

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

// handle documents one route with Req and no response body.
func handle[Req any](method, path string, h specout.Handler[Req, specout.NoContent]) func(*specout.Generator) {
	return func(d *specout.Generator) {
		h.HandlerFunc = noop
		d.Document(method, path, h)
	}
}

// panicCase is one value that must stop specout. With cfg, the generator
// uses it. With no run, the generator documents one plain route.
type panicCase struct {
	cfg  *specout.Config
	run  func(*specout.Generator)
	want string
}

// wantPanics fails unless each case panics with a specout message that has
// its want text.
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

// The jsonschema tag writes a list default as one item. JSON Schema needs the
// default to be a list, so specout writes [item].
func TestListDefaultIsAList(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/a", specout.Handler[listDefaultQuery, specout.NoContent]{HandlerFunc: noop})
	params := opOf(t, doc, "/a", "get")["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, []any{float64(1)}, child(t, params[0], "schema")["default"])

	doc = docOf(t, http.MethodPost, "/a", specout.Handler[listDefaultBody, specout.NoContent]{HandlerFunc: noop})
	assert.Equal(t, []any{true}, child(t, componentsSchema(t, doc, "listDefaultBody"), "properties", "flags")["default"])
}

// An empty struct with its own JSONSchema has a body, so the response has
// content. A GET body stays: RFC 9110 gives it no meaning, but allows it.
func TestBodiesThatStay(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/a", specout.Handler[struct{}, emptyWithJSONSchema]{HandlerFunc: noop})
	assert.Contains(t, child(t, opOf(t, doc, "/a", "get"), "responses", "200", "content"), "application/json")
	assert.Equal(t, "string", componentsSchema(t, doc, "emptyWithJSONSchema")["type"])

	doc = docOf(t, http.MethodGet, "/a", specout.Handler[plainBody, specout.NoContent]{HandlerFunc: noop})
	assert.Contains(t, child(t, opOf(t, doc, "/a", "get"), "requestBody", "content"), "application/json")
}

// A repeated operation tag names the same group, so specout writes it once.
func TestRepeatedTagsAreWrittenOnce(t *testing.T) {
	doc := docOf(t, http.MethodGet, "/a", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: noop, Tags: []string{"a", "b", "a"},
	})
	assert.Equal(t, []any{"a", "b"}, opOf(t, doc, "/a", "get")["tags"])
}
