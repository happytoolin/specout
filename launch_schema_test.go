package specout_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	json "encoding/json/v2"

	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type launchItem struct {
	Name string `json:"name"`
}

type launchEnvelope struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}

// This valid user type intentionally collides with a generated branch name.
type launchEnvelopeVariant1 struct {
	Other int `json:"other"`
}

type launchRecursiveEmbedding struct{ *launchRecursiveEmbedding }

type launchPointerCycle *launchPointerCycle

// Samples are serialized by Go before Python checks their JSON Schema contract.
type launchSample struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Value  any    `json:"value"`
	Valid  bool   `json:"valid"`
}

func exportLaunchSamples(t *testing.T, doc map[string]any, samples ...launchSample) {
	t.Helper()
	if os.Getenv("SPECOUT_VALIDATE_DIR") == "" {
		return
	}
	doc["x-specout-test-cases"] = samples
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	exportValidationDoc(t, data)
}

func launchResponseSchema(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	response := opOf(t, doc, path, method)["responses"].(map[string]any)["200"].(map[string]any)
	return response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
}

func TestLaunchUnionNameCollision(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			d := newGen()
			d.Register[schemaEmail]("email")
			d.Register[schemaSlack]("slack")
			union := func() {
				specout.Document(d, http.MethodGet, "/union", specout.Get[launchEnvelope]{HandlerFunc: noop})
			}
			other := func() {
				specout.Document(d, http.MethodGet, "/other", specout.Get[launchEnvelopeVariant1]{HandlerFunc: noop})
			}
			if reverse {
				other()
				union()
			} else {
				union()
				other()
			}
			doc := buildDoc(t, d)
			schema := launchResponseSchema(t, doc, "/union", "get")
			exportLaunchSamples(t, doc,
				launchSample{"valid union", schema, launchEnvelope{Kind: "email", Data: schemaEmail{Address: "ops@example.com"}}, true},
				launchSample{"unrelated object", schema, launchEnvelopeVariant1{Other: 1}, false},
			)
			branch := componentsSchema(t, doc, "launchEnvelope")["oneOf"].([]any)[0].(map[string]any)["$ref"]
			assert.NotEqual(t, launchResponseSchema(t, doc, "/other", "get")["$ref"], branch,
				"a union branch must not reference an unrelated response type")
		})
	}
}

func TestLaunchRootPointerNullability(t *testing.T) {
	doc := docOf(t, http.MethodPost, "/x", specout.Handler[*launchItem, *launchItem]{HandlerFunc: noop})
	op := opOf(t, doc, "/x", "post")
	body := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	response := launchResponseSchema(t, doc, "/x", "post")
	exportLaunchSamples(t, doc,
		launchSample{"nil request", body, (*launchItem)(nil), true},
		launchSample{"nil response", response, (*launchItem)(nil), true},
		launchSample{"object response", response, launchItem{Name: "valid"}, true},
	)
	assert.True(t, isNullable(body), "pointer request bodies must allow null")
	assert.True(t, isNullable(response), "nil response pointers serialize as null")
}

func TestLaunchJSONNamesIgnoreFormTags(t *testing.T) {
	type response struct {
		Name string `form:"display_name" json:"name"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	exportLaunchSamples(t, doc, launchSample{"JSON response", launchResponseSchema(t, doc, "/x", "get"), response{Name: "Alice"}, true})
	assert.Contains(t, props(t, doc, "response"), "name")
	assert.NotContains(t, props(t, doc, "response"), "display_name")
}

func TestLaunchNilEmbeddedPointer(t *testing.T) {
	type response struct{ *launchItem }
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	schema := launchResponseSchema(t, doc, "/x", "get")
	exportLaunchSamples(t, doc,
		launchSample{"nil embedding", schema, response{}, true},
		launchSample{"populated embedding", schema, response{launchItem: &launchItem{Name: "valid"}}, true},
	)
	required, _ := componentsSchema(t, doc, "response")["required"].([]any)
	assert.NotContains(t, required, "name")
}

func TestLaunchNullableWrapperFixups(t *testing.T) {
	type response struct {
		Child struct {
			Name *string `json:"name"`
		} `json:"child" jsonschema:"nullable"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	exportLaunchSamples(t, doc, launchSample{"nested nil pointer", launchResponseSchema(t, doc, "/x", "get"), response{}, true})
	child := props(t, doc, "response")["child"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)
	assert.True(t, isNullable(child["properties"].(map[string]any)["name"].(map[string]any)))
}

func TestLaunchExcludedRecursiveField(t *testing.T) {
	type response struct {
		Hidden launchRecursiveEmbedding `json:"-"`
		Name   string                   `json:"name"`
	}
	assert.NotPanics(t, func() {
		doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
		assert.Equal(t, []string{"name"}, keys(props(t, doc, "response")))
	}, "a JSON-excluded field must not prevent schema generation")
}

func TestLaunchRepeatedBooleanEnums(t *testing.T) {
	type request struct {
		Enabled bool `jsonschema:"enum=true,enum=false" query:"enabled"`
	}
	type response struct {
		Enabled bool `json:"enabled" jsonschema:"enum=true,enum=false"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Handler[request, response]{HandlerFunc: noop})
	parameter := paramsOf(t, opOf(t, doc, "/x", "get"))["enabled"]["schema"].(map[string]any)
	exportLaunchSamples(t, doc,
		launchSample{"false response", launchResponseSchema(t, doc, "/x", "get"), response{Enabled: false}, true},
		launchSample{"false parameter", parameter, false, true},
	)
	assert.ElementsMatch(t, []any{true, false}, props(t, doc, "response")["enabled"].(map[string]any)["enum"])
	assert.ElementsMatch(t, []any{true, false}, parameter["enum"])
}

func TestLaunchExcludedEmbeddedSchema(t *testing.T) {
	type response struct {
		launchItem `jsonschema:"-"`

		Visible string `json:"visible"`
	}
	doc := docOf(t, http.MethodGet, "/x", specout.Get[response]{HandlerFunc: noop})
	assert.Equal(t, []string{"visible"}, keys(props(t, doc, "response")))
	assert.NotContains(t, componentsSchema(t, doc, "response")["required"], "name")
}

func TestLaunchRecursivePointerTerminates(t *testing.T) {
	if mode := os.Getenv("SPECOUT_LAUNCH_POINTER_CYCLE"); mode != "" {
		// Unsupported types may fail explicitly, but must not loop forever.
		defer func() {
			if caught := recover(); caught != nil {
				assert.True(t, strings.HasPrefix(fmt.Sprint(caught), "specout:"), "unexpected panic: %v", caught)
			}
		}()
		d := newGen()
		if mode == "request" {
			specout.Document(d, http.MethodPost, "/x", specout.Handler[launchPointerCycle, string]{HandlerFunc: noop})
		} else {
			specout.Document(d, http.MethodGet, "/x", specout.Get[launchPointerCycle]{HandlerFunc: noop})
		}
		if err := d.WriteJSON(io.Discard); err != nil {
			t.Logf("recursive type rejected: %v", err)
		}
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"request", "response"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestLaunchRecursivePointerTerminates$")
			command.Env = append(os.Environ(), "SPECOUT_LAUNCH_POINTER_CYCLE="+mode)
			output, runErr := command.CombinedOutput()
			require.NoError(t, ctx.Err(), "schema generation hung for recursive %s pointer: %s", mode, output)
			require.NoError(t, runErr, "%s", output)
		})
	}
}

func TestLaunchNullableHelperRejectsOrdinaryUnion(t *testing.T) {
	assert.False(t, isNullable(map[string]any{"oneOf": []any{
		map[string]any{"type": "string"}, map[string]any{"type": "integer"},
	}}))
}

func TestLaunchFailedBuildCanBeRetried(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodPost, "/x", specout.Handler[launchEnvelope, specout.NoContent]{HandlerFunc: noop})
	assert.Panics(t, func() { _ = d.WriteJSON(io.Discard) })
	d.Register[schemaEmail]("email")
	d.Register[schemaSlack]("slack")
	var first, second bytes.Buffer
	require.NoError(t, d.WriteJSON(&first))
	require.NoError(t, d.WriteJSON(&second))
	assert.Equal(t, first.Bytes(), second.Bytes(), "a failed build must not poison the next build")
}

func TestLaunchConcurrentExportsAreStable(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x", specout.Get[launchItem]{HandlerFunc: noop})
	var output [32]bytes.Buffer
	var errors [32]error
	var workers sync.WaitGroup
	for i := range output {
		workers.Go(func() { errors[i] = d.WriteJSON(&output[i]) })
	}
	workers.Wait()
	for i := range output {
		require.NoError(t, errors[i])
		assert.Equal(t, output[0].Bytes(), output[i].Bytes())
	}
}
