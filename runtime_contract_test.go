package specout_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFluentBranchesAreIndependent(t *testing.T) {
	base := specout.Get[string]{
		Tags:                make([]string, 1, 4),
		Responses:           make([]specout.Response, 1, 4),
		RequestContentTypes: make([]string, 1, 4),
	}
	first := base.WithTags("first").WithResponse(specout.Response{Status: 201}).
		WithRequestContentTypes("application/json")
	second := base.WithTags("second").WithResponses(specout.Response{Status: 202}).
		WithRequestContentTypes("application/xml")
	third := base.WithResponses(specout.Response{Status: 203})
	assert.Equal(t, "first", first.Tags[1])
	assert.Equal(t, "second", second.Tags[1])
	assert.Equal(t, 201, first.Responses[1].Status)
	assert.Equal(t, 202, second.Responses[1].Status)
	assert.Equal(t, 203, third.Responses[1].Status)
	assert.Equal(t, "application/json", first.RequestContentTypes[1])
	assert.Equal(t, "application/xml", second.RequestContentTypes[1])
	assert.Len(t, base.Tags, 1)
	assert.Len(t, base.Responses, 1)
	assert.Len(t, base.RequestContentTypes, 1)
}

func TestNestedRawMapsAreDeterministic(t *testing.T) {
	var first []byte
	for range 40 {
		d := newGen()
		specout.Document(d, http.MethodGet, "/x", specout.Get[string]{
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
		var buf bytes.Buffer
		require.NoError(t, d.WriteJSON(&buf))
		if first == nil {
			first = bytes.Clone(buf.Bytes())
		}
		require.Equal(t, first, buf.Bytes(), "fresh generators must produce identical bytes")
	}
}

func TestSpecHeadHasGetHeadersWithoutBody(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", okGet)
	get := httptest.NewRecorder()
	d.ServeHTTP(get, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil))
	head := httptest.NewRecorder()
	d.ServeHTTP(head, httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/openapi.json", nil))
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, get.Header(), head.Header())
	assert.Equal(t, strconv.Itoa(get.Body.Len()), head.Header().Get("Content-Length"))
	assert.Empty(t, head.Body.Bytes())

	post := httptest.NewRecorder()
	d.ServeHTTP(post, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/openapi.json", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, post.Code)
	assert.Equal(t, "GET, HEAD", post.Header().Get("Allow"))
}
