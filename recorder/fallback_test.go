package recorder_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

type userKey struct{}

// withUser is an auth middleware outside a ServeMux: it answers 401 with no
// token, and gives the request a new context. Both hide the mux pattern.
func withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, "ada")))
	})
}

// The production handler has middleware outside the mux. Verify matches each
// request by its path, so the 200 and the 401 count for the route, and a
// literal segment wins over a parameter.
func TestVerifyMatchesRequestsBehindOuterMiddleware(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	routes := specout.Std(d, mux)
	routes.Get("/tasks/{id}", specout.Handler[struct{}, responseBody]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Responses:   []specout.Response{{Status: http.StatusOK}, {Status: http.StatusUnauthorized}},
	})
	routes.Get("/tasks/new", nc(hit204))
	rec := recorder.New(withUser(mux))

	for _, target := range []string{"/tasks/7", "/tasks/new"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer x")
		rec.ServeHTTP(discard{}, req)
	}
	serve(rec, http.MethodGet, "/tasks/7")
	serve(rec, http.MethodGet, "/missing")
	assert.Empty(t, verify(d, rec))
}

// A test that fetches the document reports no drift for the document route.
func TestVerifyIgnoresTheDocumentRoute(t *testing.T) {
	d, r := gen(), chi.NewRouter()
	routes := specout.Chi(d, r)
	routes.Get("/x", nc(hit204))
	r.Handle("/openapi.json", d)
	require.NoError(t, routes.Adopt())
	rec := recorder.New(r)
	serve(rec, http.MethodGet, "/x")
	serve(rec, http.MethodGet, "/openapi.json")
	assert.Empty(t, verify(d, rec))
}

// discard is a ResponseWriter that drops the response.
type discard struct{}

func (discard) Header() http.Header         { return http.Header{} }
func (discard) Write(b []byte) (int, error) { return len(b), nil }
func (discard) WriteHeader(int)             {}
