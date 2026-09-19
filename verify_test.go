package specout_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireDocumentedPasses(t *testing.T) {
	d, r := documentedRouter()
	specout.Chi(d, r).RequireDocumented(t)
}

func TestRequireDocumentedFailsOnStray(t *testing.T) {
	d, r := documentedRouter()
	r.Get("/debug/vars", func(http.ResponseWriter, *http.Request) {})
	r.Get("/debug/hidden", func(http.ResponseWriter, *http.Request) {})

	capt := &captureT{}
	specout.Chi(d, r).RequireDocumented(capt, specout.Skip("/debug/*"))
	require.Empty(t, capt.errs, "skip must suppress /debug/*")
}

// A plain r.Get with no skip is reported, naming the route.
func TestRequireDocumentedReportsStray(t *testing.T) {
	d, r := documentedRouter()
	r.Get("/stray", strayHandler)
	capt := &captureT{}
	specout.Chi(d, r).RequireDocumented(capt)
	require.Contains(t, strings.Join(capt.errs, ";"), "/stray", "want /stray reported")
}

func documentedRouter() (*specout.Generator, chi.Router) {
	d := specout.New(specout.Config{Title: "test", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/documented", specout.Get[struct{}]{HandlerFunc: okBody})
	return d, r
}

// Two generators on two roots each check their own router: no shared state.
func TestTwoGeneratorsTwoRoots(t *testing.T) {
	r1, r2 := chi.NewRouter(), chi.NewRouter()
	d1 := specout.New(specout.Config{Title: "one", Version: "1"})
	d2 := specout.New(specout.Config{Title: "two", Version: "1"})
	h := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody}
	specout.Chi(d1, r1).Get("/a", h)
	specout.Chi(d2, r2).Get("/b", h)
	r2.Get("/stray", strayHandler)
	c1, c2 := &captureT{}, &captureT{}
	specout.Chi(d1, r1).RequireDocumented(c1)
	specout.Chi(d2, r2).RequireDocumented(c2)
	assert.Empty(t, c1.errs, "root one reported a stray")
	assert.NotEmpty(t, c2.errs, "root two stray not reported")
}

func strayHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

type captureT struct{ errs []string }

func (c *captureT) Helper() {}
func (c *captureT) Errorf(format string, args ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}
