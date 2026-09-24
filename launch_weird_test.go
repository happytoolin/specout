package specout_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	json "encoding/json/v2"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchWeirdUnicodeRoutes(t *testing.T) {
	for _, path := range []string{"/日本語", "/éclair", "/🦀", "/items/İstanbul", "/items/a*b", "/items/*"} { //nolint:gosmopolitan // Exercise Unicode route names.
		t.Run(path, func(t *testing.T) {
			d, mux := newGen(), http.NewServeMux()
			specout.Std(d, mux).Get(path, okGet)
			w := httptest.NewRecorder()
			target := (&url.URL{Path: path}).String()
			mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Contains(t, docPaths(t, d), path)
		})
	}
}

func TestLaunchWeirdMalformedJSONNames(t *testing.T) {
	for _, name := range []string{"'a,b'", "'-'", "'a b'", "'a\\'b'"} {
		t.Run(name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{Name: "Value", Type: reflect.TypeFor[string](), Tag: reflect.StructTag("json:" + strconv.Quote(name))}})
			value := reflect.New(typ).Elem()
			value.Field(0).SetString("present")
			_, err := json.Marshal(value.Interface())
			require.Error(t, err, "control: these tags are invalid for the JSON encoder")
			field, _, _ := strings.Cut(name, ",")
			assert.PanicsWithValue(t, "specout: invalid JSON field name "+strconv.Quote(field), func() {
				docOf(t, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: value.Interface()}))
			}, "invalid JSON names must not produce a misleading schema")
		})
	}
}

func TestLaunchWeirdQuotedNumberEnum(t *testing.T) {
	type response struct {
		Value int `json:"value,string" jsonschema:"enum=1,enum=2"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"quoted numeric enum", schema, response{Value: 1}, true},
		launchSample{"outside quoted numeric enum", schema, response{Value: 3}, false},
	)
}

func TestLaunchWeirdExcludedParameter(t *testing.T) {
	type request struct {
		Value string `jsonschema:"-,description=Hidden" query:"value"`
	}
	assert.PanicsWithValue(t, "specout: field Value has no parameter schema", func() {
		docOf(t, http.MethodGet, "/x", specout.Handler[request, specout.NoContent]{HandlerFunc: noop})
	})
}

func TestLaunchWeirdDuplicateUnionNames(t *testing.T) {
	type response struct {
		Kind string `json:"kind" jsonschema:"enum=email|email,description=Discriminator"`
		Data any    `json:"data" jsonschema:"oneof_type=email|email"`
	}
	d := newGen()
	d.Register[schemaEmail]("email")
	specout.Document(d, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	exportLaunchSamples(t, doc, launchSample{"repeated union variant", launchResponseSchema(t, doc, "/x", "get"), response{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true})
}

func TestLaunchWeirdNamedJSONEmbeddingFailsClearly(t *testing.T) {
	type inner struct {
		Name *string `json:"name"`
	}
	for _, option := range []string{"inline", "embed"} {
		t.Run(option, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{Name: "Inner", Type: reflect.TypeFor[inner](), Tag: reflect.StructTag("json:" + strconv.Quote(","+option))}})
			assert.PanicsWithValue(t, "specout: explicit JSON embedding requires an anonymous struct field", func() {
				docOf(t, http.MethodGet, "/x", okGet.WithResponse(specout.Response{Status: http.StatusOK, Type: reflect.New(typ).Elem().Interface()}))
			})
		})
	}
}

func TestLaunchWeirdMixedEnumSyntax(t *testing.T) {
	type request struct {
		State string `jsonschema:"enum=ready|done,enum=done|failed" query:"state"`
	}
	type response struct {
		State string `json:"state" jsonschema:"enum=ready|done,enum=done|failed"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, response]{HandlerFunc: noop})
	assert.Equal(t, []any{"ready", "done", "failed"}, props(t, doc, "response")["state"].(map[string]any)["enum"])
	schema := launchResponseSchema(t, doc, "/x", "get")
	query := paramsOf(t, opOf(t, doc, "/x", "get"))["state"]["schema"]
	exportLaunchSamples(t, doc,
		launchSample{"first mixed enum", schema, response{State: "ready"}, true},
		launchSample{"last mixed enum", schema, response{State: "failed"}, true},
		launchSample{"mixed enum parameter", query, "done", true},
		launchSample{"literal unsplit enum", schema, response{State: "ready|done"}, false},
	)
}
