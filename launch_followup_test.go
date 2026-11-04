package specout_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
)

func TestLaunchPointerContainersAndSplitBodies(t *testing.T) {
	type request struct {
		*launchItem

		Limit int `json:"-" query:"limit"`
	}
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[*request, *[]*launchItem]{HandlerFunc: noop})
	body := opOf(t, doc, "/x", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"]
	response := launchResponseSchema(t, doc, "/x", "post")
	exportLaunchSamples(t, doc,
		launchSample{"nil split body", body, (*request)(nil), true},
		launchSample{"nil embedded body", body, request{}, true},
		launchSample{"populated body", body, request{launchItem: &launchItem{Name: "present"}}, true},
		launchSample{"nil container pointer", response, (*[]*launchItem)(nil), true},
		launchSample{"nullable array elements", response, []*launchItem{nil, {Name: "present"}}, true},
		launchSample{"wrong array element", response, []any{42}, false},
	)
}

func TestLaunchNullableTaggedContainersAndEnums(t *testing.T) {
	type response struct {
		Enabled bool               `json:"enabled" jsonschema:"nullable,enum=true,enum=false"`
		Items   []*launchItem      `json:"items"   jsonschema:"nullable"`
		Names   map[string]*string `json:"names"   jsonschema:"nullable"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"nullable tagged values", schema, map[string]any{"enabled": nil, "items": nil, "names": nil}, true},
		launchSample{"nested nullable container values", schema, response{Items: []*launchItem{nil}, Names: map[string]*string{"a": nil}}, true},
		launchSample{"invalid tagged bool", schema, map[string]any{"enabled": 42, "items": nil, "names": nil}, false},
	)
}

func TestLaunchLiteralDashRequiredField(t *testing.T) {
	type response struct {
		Value int `json:"-,string"` //nolint:staticcheck // Test the legacy spelling of a literal dash shared by JSON v1 and v2.
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"literal dash field", schema, response{Value: 42}, true},
		launchSample{"missing literal dash field", schema, map[string]any{}, false},
	)
	assert.Contains(t, componentsSchema(t, doc, "response")["required"], "-")
}

func TestLaunchLiteralDashRequiredTag(t *testing.T) {
	type response struct {
		Value string `json:"-,omitempty" jsonschema:"required"` //nolint:staticcheck // Test the legacy spelling of a literal dash.
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"required tagged dash field", schema, response{Value: "value"}, true},
		launchSample{"missing required tagged dash field", schema, map[string]any{}, false},
	)
	assert.Contains(t, componentsSchema(t, doc, "response")["required"], "-")
}

func TestLaunchFormUnionAndParameterUnion(t *testing.T) {
	type form struct {
		Kind string `form:"event"   json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
		Data any    `form:"payload" json:"data" jsonschema:"oneof_type=email|slack"`
	}
	type request struct {
		Filter launchEnvelope `jsonschema:"style=deepObject" query:"filter"`
	}
	d := newGen()
	d.Register[schemaEmail]("email")
	d.Register[schemaSlack]("slack")
	d.Document(http.MethodPost, "/form", specout.Handler[form, form]{HandlerFunc: noop}.
		WithRequest(specout.Request{ContentTypes: []string{"application/x-www-form-urlencoded"}}))
	d.Document(http.MethodGet, "/query", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	body := opOf(t, doc, "/form", "post")["requestBody"].(map[string]any)["content"].(map[string]any)["application/x-www-form-urlencoded"].(map[string]any)["schema"]
	parameter := paramsOf(t, opOf(t, doc, "/query", "get"))["filter"]["schema"]
	exportLaunchSamples(t, doc,
		launchSample{"form union", body, map[string]any{"event": "email", "payload": map[string]any{"address": "ops@example.com"}}, true},
		launchSample{"form union mismatch", body, map[string]any{"event": "email", "payload": map[string]any{"channel": "alerts"}}, false},
		launchSample{"parameter union", parameter, launchEnvelope{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true},
		launchSample{"parameter union mismatch", parameter, launchEnvelope{Kind: "email", Data: schemaSlack{Channel: "alerts"}}, false},
	)
}

func TestLaunchIgnoredRecursiveFields(t *testing.T) {
	type response struct {
		SchemaHidden launchRecursiveEmbedding `jsonschema:"-"`
		private      launchRecursiveEmbedding //nolint:unused // The schema walk must ignore unexported fields.
		Name         string                   `json:"name"`
	}
	assert.NotPanics(t, func() {
		doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
		assert.Equal(t, []string{"name"}, keys(props(t, doc, "response")))
	})
}

func TestLaunchRawStatusRangesAndDefaults(t *testing.T) {
	for _, raw := range []any{
		map[string]any{"201": map[string]any{"description": "created"}, "4XX": map[string]any{"description": "client error"}, "default": map[string]any{"description": "other"}},
		map[string]map[string]string{"201": {"description": "created"}, "4XX": {"description": "client error"}, "default": {"description": "other"}},
	} {
		d := specout.New(specout.Config{Title: "t", Version: "1", ErrorType: launchItem{}, DefaultErrors: []int{500}})
		d.Document(http.MethodGet, "/x", specout.Get[string]{HandlerFunc: noop, Raw: map[string]any{"responses": raw}})
		doc := buildDoc(t, d)
		assert.Len(t, opOf(t, doc, "/x", "get")["responses"], 3)
		key := specout.NewRouteKey(http.MethodGet, "/x")
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

func TestLaunchRawResponseMustBeAnObject(t *testing.T) {
	d := newGen()
	raw := map[string]any{"200": "ok"}
	assert.PanicsWithValue(t, "specout: GET /x: Raw.responses: 200 must be a Response object", func() {
		d.Document(http.MethodGet, "/x", specout.Get[string]{HandlerFunc: noop, Raw: map[string]any{"responses": raw}})
	})
}

func TestLaunchOperationsThatBreakTheDocument(t *testing.T) {
	get := specout.Get[string]{HandlerFunc: noop}
	for _, tc := range []struct {
		name, want string
		h          specout.Get[string]
	}{
		{
			"every response omitted", "specout: GET /x: every response is omitted; declare at least one",
			get.WithResponse(specout.Response{Status: http.StatusOK, Omit: true}),
		},
		{
			"unknown operation Raw key", `specout: GET /x: Raw key "securitySchemes" is not allowed here; start an extension with x-`,
			get.WithRaw(map[string]any{"securitySchemes": []any{}}),
		},
		{
			"unknown response Raw key", `specout: GET /x: response 200: Raw key "foo" is not allowed here; start an extension with x-`,
			get.WithResponse(specout.Response{Status: http.StatusOK, Raw: map[string]any{"foo": 1}}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.PanicsWithValue(t, tc.want, func() { newGen().Document(http.MethodGet, "/x", tc.h) })
		})
	}
}

// Route data that would break the document fails at registration, before the
// route reaches the router: the stack names the route, and neither the router
// nor the document holds it.
func TestBadRouteDataFailsAtRegistration(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	bad := specout.Get[string]{HandlerFunc: noop}.WithResponse(specout.Response{
		Status: http.StatusOK, Headers: []specout.Header{{Name: "X-Id"}, {Name: "x-id"}},
	})
	assert.PanicsWithValue(t, `specout: GET /x: response 200: header "x-id" is repeated; header names ignore case`, func() {
		specout.Std(d, mux).Get("/x", bad)
	})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
	paths, _ := buildDoc(t, d)["paths"].(map[string]any)
	assert.NotContains(t, paths, "/x")
}

func TestLaunchRawOperationIDMustBeUnique(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/a", specout.Get[string]{HandlerFunc: noop, Raw: map[string]any{"operationId": "getB"}})
	d.Document(http.MethodGet, "/b", specout.Get[string]{HandlerFunc: noop})
	wantBuildErr(t, d, `specout: GET /b: duplicate operationId "getB", also on GET /a`)
}

func TestLaunchUnionNameCollisionWithNestedComponent(t *testing.T) {
	type response struct {
		Value launchEnvelopeVariant1 `json:"value"`
	}
	d := newGen()
	d.Register[schemaEmail]("email")
	d.Register[schemaSlack]("slack")
	d.Document(http.MethodGet, "/union", specout.Get[launchEnvelope]{HandlerFunc: noop})
	d.Document(http.MethodGet, "/nested", specout.Get[response]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	schema := launchResponseSchema(t, doc, "/union", "get")
	exportLaunchSamples(t, doc,
		launchSample{"union with later nested collision", schema, launchEnvelope{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true},
		launchSample{"unrelated nested component", schema, launchEnvelopeVariant1{Other: 1}, false},
	)
}
