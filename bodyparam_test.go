package specout_test

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Req is path+query+body together, so the emitted request body must hold the
// body fields only: a $ref to the whole struct would list petId and dry_run as
// body properties too.
type patchReq struct {
	PetID int64    `jsonschema:"format=int64" path:"petId"`
	Dry   bool     `query:"dry_run"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags,omitempty"`
}

type onlyParams struct {
	ID   int64  `path:"id"`
	Sort string `query:"sort"`
}

type tagBody struct {
	Name string `json:"name"`
}

// requestBody returns the operation's single content type and its media object.
func requestBody(t *testing.T, op map[string]any) (string, map[string]any) {
	t.Helper()
	rb, ok := op["requestBody"].(map[string]any)
	require.True(t, ok, "no requestBody in %v", op)
	content, ok := rb["content"].(map[string]any)
	require.True(t, ok, "no content in %v", rb)
	require.Len(t, content, 1, "content types = %v, want one", content)
	for ct, raw := range content {
		return ct, raw.(map[string]any)
	}
	return "", nil
}

func TestBodyExcludesParamFields(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Patch("/pets/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: okBody})
	// one Req type on two routes: one body component, not a name clash
	specout.Chi(d, r).Post("/pets/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: noop})
	doc := serveDoc(t, d, r)

	item := doc["paths"].(map[string]any)["/pets/{petId}"].(map[string]any)
	op := item["patch"].(map[string]any)
	params := paramsOf(t, op)
	assert.Len(t, params, 2)
	assert.Equal(t, "path", params["petId"]["in"])
	assert.Equal(t, "query", params["dry_run"]["in"])

	ct, media := requestBody(t, op)
	assert.Equal(t, "application/json", ct)
	schema := media["schema"].(map[string]any)
	assert.Equal(t, "#/components/schemas/patchReqBody", schema["$ref"], "body schema")

	body := schemas(t, doc)["patchReqBody"].(map[string]any)
	bProps := body["properties"].(map[string]any)
	assert.Len(t, bProps, 2, "body properties = %v, want name+tags only", bProps)
	assert.Contains(t, bProps, "name")
	assert.Contains(t, bProps, "tags")
	assert.Equal(t, []any{"name"}, body["required"], "body required")

	// the POST shares the type: same component, both ops ref it
	_, postMedia := requestBody(t, item["post"].(map[string]any))
	assert.Equal(t, schema["$ref"], postMedia["schema"].(map[string]any)["$ref"])
}

// every field a parameter: no body at all.
func TestAllParamReqHasNoBody(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Post("/things/{id}", specout.Handler[onlyParams, specout.NoContent]{HandlerFunc: okBody})
	doc := serveDoc(t, d, r)
	assert.NotContains(t, opOf(t, doc, "/things/{id}", "post"), "requestBody")
}

// a non-struct Req is the whole body: an array payload, and a bare File is an
// octet-stream payload (petstore's uploadImage).
func TestNonStructRequestBody(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Post("/tags", specout.Handler[[]tagBody, specout.NoContent]{HandlerFunc: okBody})
	specout.Chi(d, r).Post("/upload", specout.Handler[specout.File, specout.NoContent]{HandlerFunc: noop})
	doc := serveDoc(t, d, r)

	ct, media := requestBody(t, opOf(t, doc, "/tags", "post"))
	assert.Equal(t, "application/json", ct)
	assert.Equal(t, "#/components/schemas/tagBodyList", media["schema"].(map[string]any)["$ref"])

	ct, media = requestBody(t, opOf(t, doc, "/upload", "post"))
	assert.Equal(t, "application/octet-stream", ct)
	schema := media["schema"].(map[string]any)
	assert.Equal(t, "string", schema["type"])
	assert.Equal(t, "binary", schema["format"])
	assert.NotContains(t, schemas(t, doc), "File")
}

// the emitted document must be valid OpenAPI: the body schema of a mixed Req
// keeps every body property in required-order and drops the params.
func TestBodySchemaJSONShape(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Post("/mixed/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: okBody})
	doc := serveDoc(t, d, r)
	raw, err := json.Marshal(schemas(t, doc)["patchReqBody"])
	require.NoError(t, err)
	got := string(raw)
	assert.Contains(t, got, `"name"`)
	assert.NotContains(t, got, "petId")
	assert.NotContains(t, got, "PetID")
}
