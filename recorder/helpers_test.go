package recorder_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/require"
)

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
	d, mux := gen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(mux)).Handle("POST", "/items", specout.Handler[struct{}, responseBody]{
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
		Responses: []specout.Response{{Status: http.StatusOK}, {Status: http.StatusCreated}, {Status: http.StatusConflict}},
	})
	return d, recorder.New(mux)
}

func serve(h http.Handler, method, target string) {
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), method, target, nil))
}

func stdRoute(d *specout.Generator, method string, h http.HandlerFunc) *recorder.Recorder {
	mux := http.NewServeMux()
	specout.Bind(d, specoutstd.New(mux)).Handle(method, "/x", nc(h))
	return recorder.New(mux)
}

func verify(d *specout.Generator, rec *recorder.Recorder) []string {
	return adaptertest.Errs(recorder.Verify(d, rec))
}

func findErr(errs []string, want string) string {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return e
		}
	}
	return ""
}

func wantNoErr(t *testing.T, d *specout.Generator, rec *recorder.Recorder, reject, label string) {
	t.Helper()
	require.Empty(t, findErr(verify(d, rec), reject), label)
}
