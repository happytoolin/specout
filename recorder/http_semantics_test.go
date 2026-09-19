package recorder_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecorderFinalStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   http.HandlerFunc
		want int
	}{
		{"empty response", func(http.ResponseWriter, *http.Request) {}, 200},
		{"body implies 200", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }, 200},
		{"first final status", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusInternalServerError)
		}, http.StatusCreated},
		{"informational then final", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusProcessing)
			w.WriteHeader(http.StatusCreated)
		}, http.StatusCreated},
		{"informational then implicit", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusEarlyHints) }, http.StatusOK},
		{"flush commits 200", func(w http.ResponseWriter, _ *http.Request) {
			_ = http.NewResponseController(w).Flush()
			w.WriteHeader(http.StatusInternalServerError)
		}, http.StatusOK},
		{"direct flush commits 200", func(w http.ResponseWriter, _ *http.Request) {
			w.(http.Flusher).Flush()
			w.WriteHeader(http.StatusInternalServerError)
		}, http.StatusOK},
		{"flush preserves final status", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_ = http.NewResponseController(w).Flush()
			w.WriteHeader(http.StatusInternalServerError)
		}, http.StatusAccepted},
		{"copy implies 200", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.Copy(w, struct{ io.Reader }{strings.NewReader("copied")})
		}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, mux := gen(), http.NewServeMux()
			specout.Std(d, mux).Get("/x", specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: tc.fn, Responses: []specout.Response{{Status: tc.want}},
			})
			rec := recorder.New(mux)
			// A real server is needed: httptest.ResponseRecorder treats 1xx as final.
			server := httptest.NewServer(rec)
			t.Cleanup(server.Close)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/x", nil)
			req.RequestURI = ""
			res, err := server.Client().Do(req)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, res.Body)
			require.NoError(t, res.Body.Close())
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.StatusCode)
			wantNoErr(t, d, rec, "", "recorded status differs from the HTTP response")
		})
	}
}

func TestRecorderPreservesWriterInterfaces(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(strconvBool(http2), func(t *testing.T) {
			server := httptest.NewUnstartedServer(recorder.New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, flush := w.(http.Flusher)
				_, hijack := w.(http.Hijacker)
				_, push := w.(http.Pusher)
				_, readFrom := w.(io.ReaderFrom)
				assert.True(t, flush)
				assert.Equal(t, !http2, hijack)
				assert.Equal(t, http2, push)
				assert.Equal(t, !http2, readFrom)
				w.WriteHeader(http.StatusNoContent)
			})))
			server.EnableHTTP2 = http2
			server.StartTLS()
			t.Cleanup(server.Close)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			req.RequestURI = ""
			res, err := server.Client().Do(req)
			require.NoError(t, err)
			require.NoError(t, res.Body.Close())
		})
	}

	minimal := struct{ http.ResponseWriter }{httptest.NewRecorder()}
	recorder.New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, flush := w.(http.Flusher)
		_, hijack := w.(http.Hijacker)
		_, push := w.(http.Pusher)
		assert.False(t, flush)
		assert.False(t, hijack)
		assert.False(t, push)
		assert.ErrorIs(t, http.NewResponseController(w).Flush(), http.ErrNotSupported)
	})).ServeHTTP(minimal, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

func strconvBool(value bool) string {
	if value {
		return "HTTP2"
	}
	return "HTTP1"
}

func TestStdTrailingSlashRecorder(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	specout.Std(d, mux).Get("/items/", nc(hit204))
	rec := recorder.New(mux)
	serve(rec, http.MethodGet, "/items/")
	wantNoErr(t, d, rec, "", "method-qualified trailing-slash route was not recorded")
}

func TestVerifyWhileRecording(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	specout.Std(d, mux).Get("/x", nc(hit204))
	rec := recorder.New(mux)
	serve(rec, http.MethodGet, "/x")
	wantNoErr(t, d, rec, "", "initial observation")
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			serve(rec, http.MethodGet, "/x")
		}
	})
	for range 100 {
		assert.Empty(t, verify(d, rec))
	}
	wg.Wait()
}
