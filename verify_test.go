package specout_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireDocumentedPasses(t *testing.T) {
	d, r := router.New()
	specout.Chi(d, r.(chi.Router)).RequireDocumented(t)
}

func TestRequireDocumentedFailsOnStray(t *testing.T) {
	d, r := router.New()
	r.(chi.Router).Get("/debug/vars", func(w http.ResponseWriter, _ *http.Request) {})
	r.(chi.Router).Get("/debug/hidden", func(w http.ResponseWriter, _ *http.Request) {})

	capt := &captureT{}
	specout.Chi(d, r.(chi.Router)).RequireDocumented(capt, specout.Skip("/debug/*"))
	require.Empty(t, capt.errs, "skip must suppress /debug/*")
}

// A plain r.Get with no skip is reported, naming the route.
func TestRequireDocumentedReportsStray(t *testing.T) {
	d, r := router.New()
	r.(chi.Router).Get("/stray", strayHandler)
	capt := &captureT{}
	specout.Chi(d, r.(chi.Router)).RequireDocumented(capt)
	require.Contains(t, strings.Join(capt.errs, ";"), "/stray", "want /stray reported")
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

func strayHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }

type captureT struct{ errs []string }

func (c *captureT) Helper() {}
func (c *captureT) Errorf(format string, args ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}
