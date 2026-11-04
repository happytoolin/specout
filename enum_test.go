package specout_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/happytoolin/specout"
)

type enumStatus string

func (enumStatus) JSONSchema() *specout.Schema { return specout.Enum[enumStatus]("todo", "done") }

type enumTask struct {
	Status enumStatus `json:"status"`
}

type enumQuery struct {
	Status enumStatus `query:"status,omitempty"`
}

// A named string type with an Enum schema shares one list in the body and in a
// query parameter.
func TestEnumIsSharedByEachUse(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/tasks", specout.Handler[enumQuery, []enumTask]{HandlerFunc: noop})
	doc := buildDoc(t, d)
	want := map[string]any{"type": "string", "enum": []any{"todo", "done"}}
	assert.Equal(t, want, schemas(t, doc)["enumStatus"])
	param := paramsOf(t, opOf(t, doc, "/tasks", "get"))["status"]
	assert.Equal(t, want, param["schema"])
}

func TestEnumNeedsUniqueValues(t *testing.T) {
	assert.Contains(t, panicMessage(t, func() { specout.Enum[string]() }), "Enum needs at least one value")
	assert.Contains(t, panicMessage(t, func() { specout.Enum("a", "b", "a") }), `value "a" is repeated`)
}
