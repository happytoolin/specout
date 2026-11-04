package specout_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/happytoolin/specout"
)

type exampleCreate struct {
	ID    string `path:"id"`
	Title string `json:"title"`
}

type exampleTask struct {
	ID string `json:"id"`
}

// An example of a named struct type must be of a type that the body can take.
// A map, raw JSON and a Req value pass.
func TestExampleTypeMustMatchTheBody(t *testing.T) {
	cfg := specout.Config{Title: "t", Version: "1", ErrorType: defaultsProblem{}}
	route := func(h specout.Handler[exampleCreate, exampleTask]) func() {
		return func() {
			h.HandlerFunc = noop
			specout.New(cfg).Document(http.MethodPost, "/x/{id}", h)
		}
	}
	for name, tc := range map[string]struct {
		h    specout.Handler[exampleCreate, exampleTask]
		want string
	}{
		"request": {
			specout.Handler[exampleCreate, exampleTask]{Request: specout.Request{Example: exampleTask{}}},
			"POST /x/{id}: Request: Example: the value is a specout_test.exampleTask, but the body is a " +
				"specout_test.exampleCreate; use a specout_test.exampleCreate value",
		},
		"named request": {
			specout.Handler[exampleCreate, exampleTask]{Request: specout.Request{
				Examples: map[string]specout.Example{"a": {Value: &exampleTask{}}},
			}},
			`Request: example "a": the value is a specout_test.exampleTask`,
		},
		"response": {
			specout.Handler[exampleCreate, exampleTask]{Responses: []specout.Response{
				{Status: http.StatusCreated, Example: defaultsProblem{}},
			}},
			"response 201: Example: the value is a specout_test.defaultsProblem, but the body is a " +
				"specout_test.exampleTask; use a specout_test.exampleTask value",
		},
		"error": {
			specout.Handler[exampleCreate, exampleTask]{Responses: []specout.Response{
				{Status: http.StatusNotFound, Example: exampleTask{}},
			}},
			"response 404: Example: the value is a specout_test.exampleTask, but the body is a specout_test.defaultsProblem",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, panicMessage(t, route(tc.h)), tc.want)
		})
	}

	for name, h := range map[string]specout.Handler[exampleCreate, exampleTask]{
		"Req value":     {Request: specout.Request{Example: exampleCreate{Title: "a"}}},
		"map":           {Request: specout.Request{Example: map[string]any{"title": "a"}}},
		"raw JSON":      {Request: specout.Request{Example: json.RawMessage(`{"title":"a"}`)}},
		"pointer":       {Responses: []specout.Response{{Status: http.StatusOK, Example: &exampleTask{}}}},
		"error body":    {Responses: []specout.Response{{Status: http.StatusNotFound, Example: defaultsProblem{}}}},
		"explicit Type": {Responses: []specout.Response{{Status: http.StatusConflict, Type: exampleTask{}, Example: exampleTask{}}}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, route(h))
		})
	}
}

// A list body takes a list example, so one item is a mistake.
func TestExampleOfAListBodyIsAList(t *testing.T) {
	msg := panicMessage(t, func() {
		specout.New(specout.Config{Title: "t", Version: "1"}).Document(http.MethodGet, "/x", specout.Get[[]exampleTask]{
			HandlerFunc: noop,
			Responses:   []specout.Response{{Status: http.StatusOK, Example: exampleTask{}}},
		})
	})
	assert.Contains(t, msg, "the value is a specout_test.exampleTask, but the body is a []specout_test.exampleTask")
}
