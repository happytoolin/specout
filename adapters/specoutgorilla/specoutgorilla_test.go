package specoutgorilla_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutgorilla"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recIDReq struct {
	ID string `path:"id"`
}

func recHit204(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func recNC(h http.HandlerFunc) specout.Handler[struct{}, specout.NoContent] {
	return specout.Handler[struct{}, specout.NoContent]{HandlerFunc: h}
}

func recServe(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return w
}

func recVerify(d *specout.Generator, rec *recorder.Recorder) []string {
	return adaptertest.Errs(recorder.Verify(d, rec))
}

func recFindErr(errs []string, want string) string {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return e
		}
	}
	return ""
}

func recRouter(skips ...string) (*specout.Generator, *mux.Router, *recorder.Recorder) {
	r := mux.NewRouter()
	rec := recorder.New(nil, skips...)
	r.Use(specoutgorilla.Record(rec))
	return adaptertest.Generator(), r, rec
}

func TestRecordPatternsCaptured(t *testing.T) {
	d, r, rec := recRouter()
	b := specout.Bind(d, specoutgorilla.New(r))
	b.Get("/a/{id}", recNC(recHit204))
	b.Get("/b/{id:[0-9]+}", recNC(recHit204))
	b.Get("/x/{id:[0-9]{4}}", recNC(recHit204))
	recServe(t, r, http.MethodGet, "/a/7")
	recServe(t, r, http.MethodGet, "/b/7")
	recServe(t, r, http.MethodGet, "/x/2024")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordUnmatchedRecordsNothing(t *testing.T) {
	d, r, rec := recRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/x", recNC(recHit204))
	recServe(t, r, http.MethodGet, "/x")
	recServe(t, r, http.MethodGet, "/nope")
	recServe(t, r, http.MethodPost, "/x")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordTrailingSlashRoutesKeepSeparateCoverage(t *testing.T) {
	d, r, rec := recRouter()
	b := specout.Bind(d, specoutgorilla.New(r.PathPrefix("/api").Subrouter()))
	b.Get("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: recHit204, OperationID: "plain"})
	b.Get("/items/", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
		OperationID: "slash",
		Responses:   []specout.Response{{Status: http.StatusAccepted}},
	})
	recServe(t, r, http.MethodGet, "/api/items/")
	assert.Equal(t, []string{"declared GET /api/items 204 never produced by any test"}, recVerify(d, rec))
	recServe(t, r, http.MethodGet, "/api/items")
	assert.Empty(t, recVerify(d, rec))
	assert.Len(t, adaptertest.Paths(t, d), 2)
}

func TestRecordCleanPathRedirect(t *testing.T) {
	d, r, rec := recRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/items", recNC(recHit204))
	require.Equal(t, http.StatusMovedPermanently, recServe(t, r, http.MethodGet, "/a/../items").Code)
	assert.Empty(t, recFindErr(recVerify(d, rec), "does not declare"))
}

func TestRecordStrictSlashRedirect(t *testing.T) {
	d, r, rec := recRouter()
	r.StrictSlash(true)
	specout.Bind(d, specoutgorilla.New(r)).Get("/dir/", recNC(recHit204))
	require.Equal(t, http.StatusMovedPermanently, recServe(t, r, http.MethodGet, "/dir").Code)
	assert.Equal(t, []string{"declared GET /dir/ 204 never produced by any test"}, recVerify(d, rec))
	recServe(t, r, http.MethodGet, "/dir/")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordMiddlewareResponse(t *testing.T) {
	d, r, rec := recRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	specout.Bind(d, specoutgorilla.New(r)).Get("/me", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: recHit204,
		Responses:   []specout.Response{{Status: http.StatusNoContent}, {Status: http.StatusUnauthorized}},
	})
	assert.Equal(t, http.StatusUnauthorized, recServe(t, r, http.MethodGet, "/me").Code)
	assert.Equal(t, []string{"declared GET /me 204 never produced by any test"}, recVerify(d, rec))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "yes")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordSkipsTheSpecEndpoint(t *testing.T) {
	d, r, rec := recRouter("/openapi.json")
	specout.Bind(d, specoutgorilla.New(r)).Get("/x", recNC(recHit204))
	r.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	recServe(t, r, http.MethodGet, "/x")
	recServe(t, r, http.MethodGet, "/openapi.json")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, r, rec := recRouter()
			specout.Bind(d, specoutgorilla.New(r)).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, r, "/ws", tc.Upgrade))
			assert.Equal(t, tc.Errs, recVerify(d, rec))
		})
	}
}

func TestRecBinderDocumentsFullTemplate(t *testing.T) {
	d, r := adaptertest.Generator(), mux.NewRouter()
	sub := r.PathPrefix("/api").Subrouter().PathPrefix("/v1").Subrouter()
	specout.Bind(d, specoutgorilla.New(sub)).Get("/users/{id:[0-9]+}", specout.Handler[recIDReq, specout.NoContent]{HandlerFunc: recHit204})
	pathItem, ok := adaptertest.Paths(t, d)["/api/v1/users/{id}"].(map[string]any)
	require.True(t, ok, "no /api/v1/users/{id} in the document")
	params := pathItem["get"].(map[string]any)["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
	assert.Equal(t, "path", params[0].(map[string]any)["in"])
	assert.Equal(t, http.StatusNoContent, recServe(t, r, http.MethodGet, "/api/v1/users/7").Code)
	assert.Equal(t, http.StatusNotFound, recServe(t, r, http.MethodGet, "/api/v1/users/x").Code)
	assert.NoError(t, specout.Check(d, specoutgorilla.New(r)))
}

func TestRecBinderPathValues(t *testing.T) {
	for _, tc := range []struct{ route, target, name, want string }{
		{"/users/{id}", "/users/42", "id", "42"},
		{"/users/{id:[0-9]+}", "/users/7", "id", "7"},
		{"/{dir}/{name}", "/d/a", "dir", "d"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, r := adaptertest.Generator(), mux.NewRouter()
			specout.Bind(d, specoutgorilla.New(r)).Get(tc.route, recNC(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Value", r.PathValue(tc.name))
				w.WriteHeader(http.StatusNoContent)
			}))
			w := recServe(t, r, http.MethodGet, tc.target)
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("X-Value"))
		})
	}
}

func TestRecBinderCheckReportsStrays(t *testing.T) {
	d, r := adaptertest.Generator(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/documented", recNC(recHit204))
	r.HandleFunc("/plain", recHit204).Methods(http.MethodGet)
	r.HandleFunc("/openapi.json", recHit204).Methods(http.MethodGet)
	r.HandleFunc("/debug/pprof", recHit204).Methods(http.MethodGet)
	r.HandleFunc("/any", recHit204)
	got := adaptertest.Strays(t, specout.Check(d, specoutgorilla.New(r), "/openapi.json", "/debug/*"))
	assert.Equal(t, []string{"? /any (no method constraint)", "GET /plain"}, got)
}

func TestRecBinderNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specout.Bind(nil, specoutgorilla.New(mux.NewRouter())) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutgorilla.New(nil)) })
}
