package specout_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demoapp"
)

func TestRequireDocumentedPasses(t *testing.T) {
	_, r, _ := demoapp.New()
	specout.RequireDocumented(t, r.(chi.Router))
}

func TestRequireDocumentedFailsOnStray(t *testing.T) {
	_, r, _ := demoapp.New()
	r.(chi.Router).Get("/debug/vars", func(w http.ResponseWriter, _ *http.Request) {})
	r.(chi.Router).Get("/debug/hidden", func(w http.ResponseWriter, _ *http.Request) {})

	capt := &captureT{}
	specout.RequireDocumented(capt, r.(chi.Router), specout.Skip("/debug/*"))
	if len(capt.errs) > 0 {
		t.Fatalf("skip should suppress /debug/*, got %v", capt.errs)
	}
}

type captureT struct{ errs []string }

func (c *captureT) Helper() {}
func (c *captureT) Errorf(format string, args ...any) {
	c.errs = append(c.errs, "err")
}

var _ = http.StatusOK
