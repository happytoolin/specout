package specout_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

// Req is path+query+body together, so the emitted request body must hold the
// body fields only: a $ref to the whole struct would list petId and dry_run as
// body properties too.
type patchReq struct {
	PetID int64    `path:"petId" jsonschema:"format=int64"`
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

func requestBody(t *testing.T, op map[string]any) (contentType string, media map[string]any) {
	t.Helper()
	rb, ok := op["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("no requestBody in %v", op)
	}
	content := rb["content"].(map[string]any)
	if len(content) != 1 {
		t.Fatalf("content types = %v, want one", content)
	}
	for ct, raw := range content {
		return ct, raw.(map[string]any)
	}
	return "", nil
}

func TestBodyExcludesParamFields(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Patch("/pets/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: okBody})
	// one Req type on two routes: one body component, not a name clash
	specout.Chi(d, r).Post("/pets/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: noop})
	doc := serveDoc(t, d, r)

	item := doc["paths"].(map[string]any)["/pets/{petId}"].(map[string]any)
	op := item["patch"].(map[string]any)
	params := map[string]string{}
	for _, raw := range op["parameters"].([]any) {
		p := raw.(map[string]any)
		params[p["name"].(string)] = p["in"].(string)
	}
	if len(params) != 2 || params["petId"] != "path" || params["dry_run"] != "query" {
		t.Fatalf("parameters = %v", params)
	}

	ct, media := requestBody(t, op)
	if ct != "application/json" {
		t.Errorf("content type = %s", ct)
	}
	schema := media["schema"].(map[string]any)
	if schema["$ref"] != "#/components/schemas/patchReq" {
		t.Fatalf("body schema = %v, want a ref to patchReq", schema)
	}
	body, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)["patchReq"].(map[string]any)
	if !ok {
		t.Fatal("patchReq component missing")
	}
	props := body["properties"].(map[string]any)
	if len(props) != 2 || props["name"] == nil || props["tags"] == nil {
		t.Errorf("body properties = %v, want name+tags only", props)
	}
	if req, _ := body["required"].([]any); len(req) != 1 || req[0] != "name" {
		t.Errorf("body required = %v, want [name] only", body["required"])
	}
	// the POST shares the type: same component, both ops ref it
	post := item["post"].(map[string]any)
	_, postMedia := requestBody(t, post)
	if ref := postMedia["schema"].(map[string]any)["$ref"]; ref != schema["$ref"] {
		t.Errorf("second route ref = %v, want %v", ref, schema["$ref"])
	}
}

// every field a parameter: no body at all.
func TestAllParamReqHasNoBody(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Post("/things/{id}", specout.Handler[onlyParams, specout.NoContent]{HandlerFunc: okBody})
	doc := serveDoc(t, d, r)
	op := doc["paths"].(map[string]any)["/things/{id}"].(map[string]any)["post"].(map[string]any)
	if _, has := op["requestBody"]; has {
		t.Errorf("requestBody = %v, want none", op["requestBody"])
	}
}

// a non-struct Req is the whole body: an array payload, and a bare File is an
// octet-stream payload (petstore's uploadImage).
func TestNonStructRequestBody(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Post("/tags", specout.Handler[[]tagBody, specout.NoContent]{HandlerFunc: okBody})
	specout.Chi(d, r).Post("/upload", specout.Handler[specout.File, specout.NoContent]{HandlerFunc: noop})
	doc := serveDoc(t, d, r)
	paths := doc["paths"].(map[string]any)

	ct, media := requestBody(t, paths["/tags"].(map[string]any)["post"].(map[string]any))
	if ct != "application/json" {
		t.Errorf("array body content type = %s", ct)
	}
	if ref := media["schema"].(map[string]any)["$ref"]; ref != "#/components/schemas/tagBodyList" {
		t.Errorf("array body schema = %v", ref)
	}

	ct, media = requestBody(t, paths["/upload"].(map[string]any)["post"].(map[string]any))
	if ct != "application/octet-stream" {
		t.Errorf("file body content type = %s", ct)
	}
	schema := media["schema"].(map[string]any)
	if schema["type"] != "string" || schema["format"] != "binary" {
		t.Errorf("file body schema = %v", schema)
	}
	if _, junk := doc["components"].(map[string]any)["schemas"].(map[string]any)["File"]; junk {
		t.Error("File leaked into components.schemas")
	}
}

// the emitted document must be valid OpenAPI: the body schema of a mixed Req
// keeps every body property in required-order and drops the params.
func TestBodySchemaJSONShape(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Post("/mixed/{petId}", specout.Handler[patchReq, specout.NoContent]{HandlerFunc: okBody})
	doc := serveDoc(t, d, r)
	body := doc["components"].(map[string]any)["schemas"].(map[string]any)["patchReq"]
	raw, _ := json.Marshal(body)
	if got := string(raw); !strings.Contains(got, `"name"`) || strings.Contains(got, "petId") || strings.Contains(got, "PetID") {
		t.Errorf("body schema = %s", got)
	}
}
