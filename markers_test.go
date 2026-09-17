package specout_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type uploadReq struct {
	File specout.File `json:"file" jsonschema:"description=CSV"`
}
type emptyMarker struct{}
type item struct {
	ID string `json:"id"`
}

// TestMarkerTypes: NoContent reads as 204, File turns the request into
// multipart with a binary property, Header declares a response header.
func TestMarkerTypes(t *testing.T) {
	d := newGen()
	r := chi.NewRouter()
	b := specout.Chi(d, r)
	b.Post("/files", specout.Handler[uploadReq, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
	})
	b.Post("/items", specout.Handler[emptyMarker, item]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {},
		Responses: []specout.Response{
			{Status: 201, Headers: []specout.Header{{Name: "Location"}}},
		},
	})
	doc := serveDoc(t, d, r)
	paths := pathsObj(doc)

	// NoContent -> 204
	files := paths["/files"].(map[string]any)["post"].(map[string]any)
	assert.Contains(t, files["responses"].(map[string]any), "204", "NoContent did not produce 204")
	// File -> multipart + binary property
	content := files["requestBody"].(map[string]any)["content"].(map[string]any)
	require.Contains(t, content, "multipart/form-data", "File did not produce multipart")
	fileProps := doc["components"].(map[string]any)["schemas"].(map[string]any)["uploadReq"].(map[string]any)["properties"].(map[string]any)
	f := fileProps["file"].(map[string]any)
	assert.Equal(t, "binary", f["format"], "File property format")
	assert.Equal(t, "string", f["type"], "File property type")

	// Header -> Location declared on 201
	items := paths["/items"].(map[string]any)["post"].(map[string]any)
	hdrs := items["responses"].(map[string]any)["201"].(map[string]any)["headers"].(map[string]any)
	assert.Contains(t, hdrs, "Location")
}
