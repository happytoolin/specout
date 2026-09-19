package recorder_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/require"
)

// failT collects failures instead of stopping the test, so a test can inspect
// them. It is the TestingT that recorder.Verify writes to.
type failT struct{ errs []string }

func (f *failT) Helper()                    {}
func (f *failT) Errorf(fm string, a ...any) { f.errs = append(f.errs, fmt.Sprintf(fm, a...)) }

func hit204(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func gen() *specout.Generator { return specout.New(specout.Config{Title: "t", Version: "1"}) }

func nc(h http.HandlerFunc) specout.Handler[struct{}, specout.NoContent] {
	return specout.Handler[struct{}, specout.NoContent]{HandlerFunc: h}
}

type responseBody struct {
	OK bool `json:"ok"`
}

func statusRoute(t *testing.T) (*specout.Generator, *recorder.Recorder) {
	t.Helper()
	d, r := gen(), chi.NewRouter()
	b := specout.Chi(d, r)
	b.Post("/items", specout.Handler[struct{}, responseBody]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("status") {
			case "201":
				w.WriteHeader(http.StatusCreated)
			case "409":
				w.WriteHeader(http.StatusConflict)
			default:
				w.WriteHeader(http.StatusOK)
			}
		},
		Responses: []specout.Response{{Status: http.StatusCreated}, {Status: http.StatusConflict}},
	})
	require.NoError(t, b.Adopt())
	return d, recorder.New(r)
}

func serve(h http.Handler, method, target string) {
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), method, target, nil))
}

// chiRoute registers /x on a fresh chi router, adopts it into d, and returns
// the recorder wrapping that router.
func chiRoute(t *testing.T, d *specout.Generator, method string, h http.HandlerFunc) *recorder.Recorder {
	t.Helper()
	r := chi.NewRouter()
	b := specout.Chi(d, r)
	if method == http.MethodHead {
		b.Head("/x", nc(h))
	} else {
		b.Get("/x", nc(h))
	}
	require.NoError(t, b.Adopt())
	return recorder.New(r)
}

// verify collects the drift failures a recorder produces against d.
func verify(d *specout.Generator, rec *recorder.Recorder) []string {
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	return ft.errs
}

// findErr returns the first failure containing want, or "" for none. An empty
// want matches any failure.
func findErr(errs []string, want string) string {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return e
		}
	}
	return ""
}

// wantNoErr fails t on a Verify failure containing reject. An empty reject
// means any failure is a bug.
func wantNoErr(t *testing.T, d *specout.Generator, rec *recorder.Recorder, reject, label string) {
	t.Helper()
	require.Empty(t, findErr(verify(d, rec), reject), label)
}
