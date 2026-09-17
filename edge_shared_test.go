package specout_test

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

type S1 struct{ V string }

// shared handler, same relative pattern inside two different groups —
// the suffix-match trap. /a/x and /b/x must both appear, with the right ops.
func TestSharedHandlerTwoGroups(t *testing.T) {
	h := specout.Handler[struct{}, S1]{HandlerFunc: noop, Summary: "shared"}
	d, r := newGen(), chi.NewRouter()
	for _, group := range []string{"/a", "/b"} {
		r.Route(group, func(r chi.Router) { specout.Chi(d, r).Get("/x", h) })
	}
	// adopting twice stays one walk source, not two
	adopt(t, specout.Chi(d, r))
	adopt(t, specout.Chi(d, r))

	doc := serve(t, d, r)
	// both paths exist and carry the shared operation
	for _, p := range []string{"/a/x", "/b/x"} {
		if got := opOf(t, doc, p, "get")["summary"]; got != "shared" {
			t.Errorf("%s summary = %v, want shared", p, got)
		}
	}
}
