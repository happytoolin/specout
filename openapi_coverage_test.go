package specout_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const coverageTable = "skills/specout/references/openapi-coverage.md"

// schemaFields lists the fields of the official 3.1 schema, keyed by the
// schema's object names, as "object.field". Conditional fields, such as the
// flows of a security scheme, sit deeper in an object's definition.
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
		// styles-for-form is a shared rule: its fields sit on parameter and
		// encoding. schema is JSON Schema, not an OpenAPI object.
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
	coverageRow     = regexp.MustCompile("^\\| `([^`]+)` \\| (Yes|Raw|In place|No) \\|")
)

// tableFields reads the coverage table into "object.field" -> status.
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

// coverageChildren maps a field of an object to the object it holds. A "[]"
// suffix is a list, a "{}" suffix is a map, and "*" is any field.
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

// oauthFlowNames are the flows of an OAuth 2.0 scheme.
var oauthFlowNames = strings.Fields("implicit password clientCredentials authorizationCode")

// aliased records a field on the schema object that defines it, when another
// object reuses that definition.
var aliased = map[string]string{"media-type.example": "examples.example", "media-type.examples": "examples.examples"}

// seenFields walks doc and records each field it finds, as "object.field".
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

const coverageFlowURL = "https://example.com/flow"

// coverageFlows declares every OAuth 2.0 flow with every URL.
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

// coverageGenerator builds a document that uses every Yes and Raw field of the
// coverage table.
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
			{Name: "jwt", Type: "httpBearer", BearerFormat: "JWT", Description: "A signed token"},
			{Name: "mtls", Type: "mutualTLS"},
			specout.APIKey("key", "X-API-Key", specout.InHeader),
			{Name: "oidc", Type: "openIdConnect", URL: "https://example.com/.well-known/openid-configuration"},
			specout.OAuth2("oauth", coverageFlows()),
		},
		Raw: map[string]any{
			"jsonSchemaDialect": "https://spec.openapis.org/oas/3.1/dialect/base",
			"webhooks": map[string]any{"itemAdded": map[string]any{"post": map[string]any{
				"responses": map[string]any{"200": map[string]any{"description": "OK"}},
			}}},
			"x-owner": "team",
		},
	})
	specout.Document(d, http.MethodPut, "/items/{id}", specout.Handler[coverageRequest, coverageItem]{
		HandlerFunc: noop, Tags: []string{"items"}, Summary: "Update", Description: "Updates an item.",
		OperationID: "updateItem", Deprecated: true,
		ExternalDocs:       &specout.ExternalDocs{URL: "https://example.com/update"},
		Security:           []specout.Security{{Scheme: "oauth", Scopes: []string{"read"}}, {Scheme: "key"}},
		RequestDescription: "The new item", RequestOptional: true,
		RequestExamples: map[string]specout.Example{
			"short": {Summary: "Short", Description: "A short name", Value: coverageItem{Name: "a"}},
			"file":  {ExternalValue: "https://example.com/item.json"},
		},
		Responses: []specout.Response{
			{
				Status: http.StatusOK, Description: "The item", Example: coverageItem{Name: "a"},
				Headers: []specout.Header{{Name: "X-Old", Description: "Old", Required: true, Deprecated: true}},
				Raw: map[string]any{"links": map[string]any{
					"byID":  map[string]any{"operationId": "updateItem", "parameters": map[string]any{"id": "$response.body#/name"}, "requestBody": "$request.body", "description": "Again"},
					"byRef": map[string]any{"operationRef": "#/paths/~1items~1{id}/put"},
				}},
			},
			{Status: 0, Key: "default", Type: coverageItem{}},
		},
		Raw: map[string]any{
			"servers":   []any{map[string]any{"url": "https://example.com"}},
			"callbacks": map[string]any{"done": map[string]any{"{$request.body#/url}": map[string]any{"post": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "OK"}}}}}},
		},
	})
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace} {
		specout.Document(d, method, "/items", specout.Get[coverageItem]{HandlerFunc: noop, Public: true})
	}
	specout.Document(d, http.MethodPost, "/upload", specout.Handler[coverageItem, coverageItem]{
		HandlerFunc: noop,
		Raw: map[string]any{"requestBody": map[string]any{"content": map[string]any{"multipart/form-data": map[string]any{
			"schema": map[string]any{"type": "object"},
			"encoding": map[string]any{"file": map[string]any{
				"contentType": "image/png", "headers": map[string]any{}, "style": "form", "explode": true, "allowReserved": true,
			}},
		}}}},
		Responses: []specout.Response{{Status: http.StatusOK, Examples: map[string]specout.Example{"one": {Value: coverageItem{Name: "a"}}}}},
	})
	specout.Document(d, http.MethodPost, "/once", specout.Handler[coverageItem, coverageItem]{HandlerFunc: noop, RequestExample: coverageItem{Name: "a"}})
	return d
}

// linkServerDoc holds the server field of a link. It stays out of the exported
// validation documents: openapi-spec-validator bundles the 2022-10-07 schema,
// which names this field body. The 2025-09-15 schema corrects the name.
func linkServerDoc(t *testing.T) map[string]any {
	t.Helper()
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Handler[struct{}, coverageItem]{
		HandlerFunc: noop,
		Responses: []specout.Response{{Status: http.StatusOK, Raw: map[string]any{"links": map[string]any{
			"next": map[string]any{"operationId": "getX", "server": map[string]any{"url": "https://example.com"}},
		}}}},
	})
	var buf bytes.Buffer
	require.NoError(t, d.WriteJSON(&buf))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	return doc
}

func TestOpenAPICoverage(t *testing.T) {
	table := tableFields(t)
	want := schemaFields(t)
	assert.Equal(t, want, slices.Sorted(maps.Keys(table)), "the table must list every field of the official schema, and no other")

	seen := make(map[string]bool)
	seenFields("root", buildDoc(t, coverageGenerator()), seen)
	// License takes a URL or an Identifier, so the URL needs its own document.
	licensed := specout.New(specout.Config{
		Title: "t", Version: "1", License: &specout.License{Name: "MIT", URL: "https://opensource.org/license/mit"},
	})
	seenFields("root", buildDoc(t, licensed), seen)
	seenFields("root", linkServerDoc(t), seen)
	for _, field := range want {
		status := table[field]
		if status == "Yes" || status == "Raw" {
			assert.True(t, seen[field], "%s is marked %s, but the test document does not have it", field, status)
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
	assert.Equal(t, map[string]any{"name": "a"}, media["example"])

	params, ok := put["parameters"].([]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{
		"name": "old", "in": "query", "required": false, "deprecated": true, "schema": map[string]any{"type": "string"},
	}, params[1])
	assert.Equal(t, map[string]any{
		"name": "empty", "in": "query", "required": false, "allowEmptyValue": true, "schema": map[string]any{"type": "string"},
	}, params[2])

	assert.Equal(t, "https://spec.openapis.org/oas/3.1/dialect/base", doc["jsonSchemaDialect"])
	assert.Equal(t, "team", doc["x-owner"])
}

// A HEAD response has no body, so its examples go with the content.
func TestHeadDropsResponseExamples(t *testing.T) {
	doc := docOf(t, http.MethodHead, "/x", specout.Handler[struct{}, coverageItem]{
		HandlerFunc: noop,
		Responses:   []specout.Response{{Status: http.StatusOK, Example: coverageItem{Name: "a"}}},
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
			specout.Document(d, http.MethodPost, "/x", h)
		}
	}
	for want, call := range map[string]func(){
		"sets both URL and Identifier": config(specout.Config{License: &specout.License{Name: "a", URL: "u", Identifier: "i"}}),
		"needs a Default":              config(specout.Config{Servers: []specout.Server{{URL: "u", Variables: map[string]specout.ServerVariable{"a": {}}}}}),
		"Enum does not hold":           config(specout.Config{Servers: []specout.Server{{URL: "u", Variables: map[string]specout.ServerVariable{"a": {Default: "x", Enum: []string{"y"}}}}}}),
		"has no Scheme":                config(specout.Config{Auth: []specout.AuthScheme{{Name: "a", Type: "http"}}}),
		"sets BearerFormat":            config(specout.Config{Auth: []specout.AuthScheme{{Name: "a", Type: "http", Scheme: "basic", BearerFormat: "JWT"}}}),
		`Raw key "paths"`:              config(specout.Config{Raw: map[string]any{"paths": 1}}),
		"does not declare":             route(specout.Handler[coverageItem, coverageItem]{Security: []specout.Security{{Scheme: "other"}}}),
		"both Public and Security":     route(specout.Handler[coverageItem, coverageItem]{Public: true, Security: []specout.Security{{Scheme: "bearerAuth"}}}),
		"sets both Example":            route(specout.Handler[coverageItem, coverageItem]{RequestExample: 1, RequestExamples: map[string]specout.Example{"a": {}}}),
		"both Value and ExternalValue": route(specout.Handler[coverageItem, coverageItem]{RequestExamples: map[string]specout.Example{"a": {Value: 1, ExternalValue: "u"}}}),
		"cannot be encoded":            route(specout.Handler[coverageItem, coverageItem]{RequestExample: make(chan int)}),
		"sets an example but has no body": route(specout.Handler[coverageItem, coverageItem]{
			Responses: []specout.Response{{Status: http.StatusNoContent, Example: 1}},
		}),
	} {
		assert.Contains(t, panicMessage(t, call), want)
	}
}

// panicMessage returns the message that call panics with.
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
	}), "allowEmptyValue=true")
	assert.Contains(t, panicMessage(t, func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[word, coverageItem]{HandlerFunc: noop})
	}), "allowReserved=maybe")
}
