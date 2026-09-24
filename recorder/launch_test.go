package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchRawResponsesCannotBypassVerification(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	specout.Std(d, mux).Get("/x", specout.Get[string]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Raw: map[string]any{"responses": map[string]any{
			"201": map[string]any{"description": "Created"},
		}},
	})
	rec := recorder.New(mux)
	serve(rec, http.MethodGet, "/x")
	assert.NotEmpty(t, findErr(verify(d, rec), "spec does not declare it"),
		"the emitted operation declares 201 only, but the handler returned 200")
}

func TestLaunchChiGetHeadCoverage(t *testing.T) {
	d, router := gen(), chi.NewRouter()
	router.Use(middleware.GetHead)
	binder := specout.Chi(d, router)
	binder.Get("/x", nc(hit204))
	require.NoError(t, binder.Adopt())
	rec := recorder.New(router)
	w := httptest.NewRecorder()
	rec.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/x", nil))
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, verify(d, rec), "the declared GET handler did run")
}

func TestLaunchChiGetHeadCannotHideDrift(t *testing.T) {
	d, router := gen(), chi.NewRouter()
	router.Use(middleware.GetHead)
	binder := specout.Chi(d, router)
	binder.Get("/x", nc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, binder.Adopt())
	rec := recorder.New(router)
	serve(rec, http.MethodGet, "/x")
	serve(rec, http.MethodHead, "/x")
	assert.NotEmpty(t, findErr(verify(d, rec), "500 but spec does not declare it"),
		"a passing GET test must not hide an undeclared HEAD failure")
}

func TestLaunchChiPathRewriteCannotHideDrift(t *testing.T) {
	d, router := gen(), chi.NewRouter()
	router.Use(middleware.StripSlashes)
	binder := specout.Chi(d, router)
	binder.Get("/x", nc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, binder.Adopt())
	rec := recorder.New(router)
	serve(rec, http.MethodGet, "/x")
	w := httptest.NewRecorder()
	rec.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/?fail", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code, "the route must actually execute")
	assert.NotEmpty(t, findErr(verify(d, rec), "500 but spec does not declare it"))
}

func TestLaunchChiUnmatchedMountIsNotAnOperation(t *testing.T) {
	d, router, sub := gen(), chi.NewRouter(), chi.NewRouter()
	specout.Chi(d, sub).Get("/x", nc(hit204))
	router.Mount("/api", sub)
	require.NoError(t, specout.Chi(d, router).Adopt())
	rec := recorder.New(router)
	serve(rec, http.MethodGet, "/api/x")
	serve(rec, http.MethodGet, "/api/missing")
	serve(rec, http.MethodHead, "/api/x")
	assert.Empty(t, verify(d, rec), "an unmatched subroute must not record its parent mount as an operation")
}

func TestLaunchChiMountedRewriteAndExplicitHead(t *testing.T) {
	d, router, sub := gen(), chi.NewRouter(), chi.NewRouter()
	router.Use(middleware.GetHead)
	sub.Use(middleware.StripSlashes)
	b := specout.Chi(d, sub)
	b.Get("/x", nc(hit204))
	b.Head("/x", nc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }).
		WithResponse(specout.Response{Status: http.StatusAccepted}))
	router.Mount("/api", sub)
	require.NoError(t, specout.Chi(d, router).Adopt())
	rec := recorder.New(router)
	serve(rec, http.MethodGet, "/api/x/")
	serve(rec, http.MethodHead, "/api/x")
	assert.Empty(t, verify(d, rec))
}
