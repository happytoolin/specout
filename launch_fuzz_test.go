package specout_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	json "encoding/json/v2"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func FuzzLaunchUnicodeLiteralPaths(f *testing.F) {
	for _, segment := range []string{"日本語", "éclair", "🦀", "İstanbul", "a*b", "*", "hello"} { //nolint:gosmopolitan // Exercise Unicode route names.
		f.Add(segment)
	}
	f.Fuzz(func(t *testing.T, segment string) {
		if segment == "" || len(segment) > 64 || !utf8.ValidString(segment) || segment == "." || segment == ".." ||
			strings.ContainsAny(segment, "/{}%?#") || strings.ContainsFunc(segment, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			t.Skip("not a bounded literal path segment")
		}
		path := "/literal/" + segment
		d, mux := newGen(), http.NewServeMux()
		specout.Std(d, mux).Get(path, okGet)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, (&url.URL{Path: path}).String(), nil))
		require.Equal(t, http.StatusNoContent, w.Code)
		assert.Contains(t, docPaths(t, d), path)
	})
}

// Use the actual JSON encoder as the oracle for property names.
func FuzzLaunchJSONFieldNames(f *testing.F) {
	for _, name := range []string{"value", "with-dash", "a/b", "$value", "with space", ""} {
		f.Add(name)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 64 || !utf8.ValidString(name) || strings.Contains(name, ",") {
			t.Skip("not a bounded, single JSON field name")
		}
		typ := reflect.StructOf([]reflect.StructField{{
			Name: "Value", Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag("json:" + strconv.Quote(name+",omitempty")),
		}})
		value := reflect.New(typ).Elem()
		value.Field(0).SetString("nonempty")
		encoded, err := json.Marshal(value.Interface())
		if err != nil {
			t.Skipf("the JSON encoder rejects this tag: %v", err)
		}
		var actual map[string]any
		require.NoError(t, json.Unmarshal(encoded, &actual))
		doc := docOf(t, http.MethodGet, "/x", okGet.WithResponse(specout.Response{
			Status: http.StatusOK, Type: value.Interface(),
		}))
		ref := launchResponseSchema(t, doc, "/x", "get")["$ref"].(string)
		component := componentsSchema(t, doc, strings.TrimPrefix(ref, "#/components/schemas/"))
		properties, _ := component["properties"].(map[string]any)
		assert.Equal(t, keys(actual), keys(properties), "JSON field names must match the specification")
	})
}

// Escaped literal segments are valid ServeMux routes, including punctuation.
func FuzzLaunchStdLiteralPaths(f *testing.F) {
	for _, segment := range []string{"hello", "with space", "$value", "a/b", "dot.name"} {
		f.Add(segment)
	}
	f.Fuzz(func(t *testing.T, segment string) {
		if segment == "" || len(segment) > 64 || !utf8.ValidString(segment) || segment == "." || segment == ".." {
			t.Skip("not a bounded literal segment")
		}
		path := "/literal/" + url.PathEscape(segment)
		d, mux := newGen(), http.NewServeMux()
		specout.Std(d, mux).Get(path, okGet)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		require.Equal(t, http.StatusNoContent, w.Code)
		assert.Contains(t, docPaths(t, d), path, "a working literal route must not disappear from the specification")
	})
}

func TestLaunchStdLiteralAsteriskIsDocumented(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	const path = "/literal/a*b"
	specout.Std(d, mux).Get(path, okGet)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Contains(t, docPaths(t, d), path, "ServeMux treats an asterisk as a literal, not a wildcard")
}

// Independently derive allowed codes from emitted response keys.
func FuzzLaunchResponsePlanParity(f *testing.F) {
	f.Add(uint16(201), uint16(404), false, false)
	f.Add(uint16(204), uint16(500), true, true)
	f.Add(uint16(304), uint16(400), false, true)
	f.Fuzz(func(t *testing.T, success, failure uint16, omitDefault, ranged bool) {
		code := 200 + int(success%100)
		errorCode := 400 + int(failure%200)
		response := specout.Response{Status: code}
		if ranged {
			response.Key = "2XX"
		}
		d := specout.New(specout.Config{
			Title: "t", Version: "1", ErrorType: launchItem{}, DefaultErrors: []int{errorCode},
		})
		handler := specout.Get[string]{HandlerFunc: noop, Responses: []specout.Response{response}}
		if omitDefault && (code != http.StatusOK || ranged) {
			handler = handler.WithResponse(specout.Response{Status: http.StatusOK, Omit: true})
		}
		specout.Document(d, http.MethodGet, "/x", handler)
		doc := buildDoc(t, d)
		want := map[int]bool{}
		for key := range opOf(t, doc, "/x", "get")["responses"].(map[string]any) {
			if key == "2XX" {
				for status := 200; status < 300; status++ {
					want[status] = true
				}
				continue
			}
			status, err := strconv.Atoi(key)
			require.NoError(t, err)
			want[status] = true
		}
		got := specStatuses(t, d)[specout.NewRouteKey(http.MethodGet, "/x")]
		assert.Equal(t, want, got)
	})
}
