package specout_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

type S1 struct{ V string }

func noop3(w http.ResponseWriter, r *http.Request) {}

// shared handler, same relative pattern inside two different groups —
// the suffix-match trap. /a/x and /b/x must both appear, with the right ops.
func TestSharedHandlerTwoGroups(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	h := specout.Handler[struct{}, S1]{HandlerFunc: noop3, Summary: "shared"}
	r.Route("/a", func(r chi.Router) { specout.Chi(d, r).Get("/x", h) })
	r.Route("/b", func(r chi.Router) { specout.Chi(d, r).Get("/x", h) })
	if err := specout.Chi(d, r).Adopt(); err != nil {
		t.Fatal(err)
	}
	if err := specout.Chi(d, r).Adopt(); err != nil {
		t.Fatal(err)
	}
	r.Mount("/openapi.json", d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc map[string]any
	json.Unmarshal(w.Body.Bytes(), &doc)
	paths := doc["paths"].(map[string]any)
	for _, want := range []string{"/a/x", "/b/x"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("%s missing; have %v", want, paths)
		}
	}
	// both carry the shared operation
	for _, p := range []string{"/a/x", "/b/x"} {
		op := paths[p].(map[string]any)["get"].(map[string]any)
		if op["summary"] != "shared" {
			t.Errorf("%s summary = %v", p, op["summary"])
		}
	}
}
