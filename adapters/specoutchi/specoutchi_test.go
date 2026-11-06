package specoutchi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutchi"
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

func recRouter(skips ...string) (*specout.Generator, *chi.Mux, *recorder.Recorder) {
	r := chi.NewRouter()
	rec := recorder.New(nil, skips...)
	r.Use(specoutchi.Record(rec))
	return adaptertest.Generator(), r, rec
}

func TestRecordEncodedSlash(t *testing.T) {
	d, r, rec := recRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/items/{id}", recNC(recHit204))
	assert.Equal(t, http.StatusNoContent, recServe(t, r, http.MethodGet, "/items/a%2Fb").Code)
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordRouteRootPath(t *testing.T) {
	for _, target := range []string{"/users", "/users/"} {
		t.Run(target, func(t *testing.T) {
			d, r, rec := recRouter()
			r.Route("/users", func(cr chi.Router) {
				b := specout.Bind(d, specoutchi.New(cr, "/users"))
				b.Get("/", recNC(recHit204))
			})
			recServe(t, r, http.MethodGet, target)
			assert.Empty(t, recVerify(d, rec))
		})
	}
}

func TestRecordUnmatchedMountStub(t *testing.T) {
	d, r, rec := recRouter()
	r.Route("/api", func(cr chi.Router) {
		b := specout.Bind(d, specoutchi.New(cr, "/api"))
		b.Get("/x", recNC(recHit204))
	})
	recServe(t, r, http.MethodGet, "/api")
	recServe(t, r, http.MethodGet, "/api/x")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordGetHeadCoverage(t *testing.T) {
	d, r, rec := recRouter()
	r.Use(middleware.GetHead)
	specout.Bind(d, specoutchi.New(r)).Get("/x", recNC(recHit204))
	assert.Equal(t, http.StatusNoContent, recServe(t, r, http.MethodHead, "/x").Code)
	assert.Empty(t, recVerify(d, rec), "the declared GET handler did run")
}

func TestRecordGetHeadCannotHideDrift(t *testing.T) {
	d, r, rec := recRouter()
	r.Use(middleware.GetHead)
	specout.Bind(d, specoutchi.New(r)).Get("/x", recNC(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	recServe(t, r, http.MethodGet, "/x")
	recServe(t, r, http.MethodHead, "/x")
	assert.NotEmpty(t, recFindErr(recVerify(d, rec), "500 but spec does not declare it"),
		"a passing GET test must not hide an undeclared HEAD failure")
}

func TestRecordPathRewriteCannotHideDrift(t *testing.T) {
	d, r, rec := recRouter()
	r.Use(middleware.StripSlashes)
	specout.Bind(d, specoutchi.New(r)).Get("/x", recNC(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	recServe(t, r, http.MethodGet, "/x")
	require.Equal(t, http.StatusInternalServerError, recServe(t, r, http.MethodGet, "/x/?fail").Code,
		"the route must actually execute")
	assert.NotEmpty(t, recFindErr(recVerify(d, rec), "500 but spec does not declare it"))
}

func TestRecordUnmatchedSubroute(t *testing.T) {
	d, r, rec := recRouter()
	r.Route("/api", func(cr chi.Router) {
		b := specout.Bind(d, specoutchi.New(cr, "/api"))
		b.Get("/x", recNC(recHit204))
	})
	recServe(t, r, http.MethodGet, "/api/x")
	recServe(t, r, http.MethodGet, "/api/missing")
	recServe(t, r, http.MethodHead, "/api/x")
	assert.Empty(t, recVerify(d, rec), "an unmatched subroute must not record its parent mount as an operation")
}

func TestRecordRewriteAndExplicitHeadInRoute(t *testing.T) {
	d, r, rec := recRouter()
	r.Use(middleware.GetHead, middleware.StripSlashes)
	r.Route("/api", func(cr chi.Router) {
		b := specout.Bind(d, specoutchi.New(cr, "/api"))
		b.Get("/x", recNC(recHit204))
		b.Head("/x", specout.Handler[struct{}, specout.NoContent]{
			HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
			Responses:   []specout.Response{{Status: http.StatusAccepted}},
		})
	})
	recServe(t, r, http.MethodGet, "/api/x/")
	recServe(t, r, http.MethodHead, "/api/x")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordMiddlewareResponseBeforeRouting(t *testing.T) {
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
	r.Route("/api", func(cr chi.Router) {
		b := specout.Bind(d, specoutchi.New(cr, "/api"))
		b.Get("/me", specout.Handler[struct{}, specout.NoContent]{
			HandlerFunc: recHit204,
			Responses:   []specout.Response{{Status: http.StatusNoContent}, {Status: http.StatusUnauthorized}},
		})
	})
	assert.Equal(t, http.StatusUnauthorized, recServe(t, r, http.MethodGet, "/api/me").Code)
	assert.Equal(t, []string{"declared GET /api/me 204 never produced by any test"}, recVerify(d, rec))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "yes")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordTrailingSlashRoutesKeepSeparateCoverage(t *testing.T) {
	d, r, rec := recRouter()
	r.Route("/api", func(cr chi.Router) {
		b := specout.Bind(d, specoutchi.New(cr, "/api"))
		b.Get("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: recHit204, OperationID: "plain"})
		b.Get("/items/", specout.Handler[struct{}, specout.NoContent]{
			HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
			OperationID: "slash",
			Responses:   []specout.Response{{Status: http.StatusAccepted}},
		})
	})
	recServe(t, r, http.MethodGet, "/api/items/")
	assert.Equal(t, []string{"declared GET /api/items 204 never produced by any test"}, recVerify(d, rec))
	recServe(t, r, http.MethodGet, "/api/items")
	assert.Empty(t, recVerify(d, rec))
	assert.Len(t, adaptertest.Paths(t, d), 2)
}

func TestRecordSkipsTheSpecEndpoint(t *testing.T) {
	d, r, rec := recRouter("/openapi.json")
	specout.Bind(d, specoutchi.New(r)).Get("/x", recNC(recHit204))
	r.Get("/openapi.json", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	recServe(t, r, http.MethodGet, "/x")
	recServe(t, r, http.MethodGet, "/openapi.json")
	assert.Empty(t, recVerify(d, rec))
}

func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, r, rec := recRouter()
			specout.Bind(d, specoutchi.New(r)).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, r, "/ws", tc.Upgrade))
			assert.Equal(t, tc.Errs, recVerify(d, rec))
		})
	}
}

func TestRecBinderDocumentsRoutePrefixes(t *testing.T) {
	d, r := adaptertest.Generator(), chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		api.Route("/v1", func(v1 chi.Router) {
			specout.Bind(d, specoutchi.New(v1, "/api/v1")).Get("/users/{id}", specout.Handler[recIDReq, specout.NoContent]{HandlerFunc: recHit204})
		})
	})
	pathItem, ok := adaptertest.Paths(t, d)["/api/v1/users/{id}"].(map[string]any)
	require.True(t, ok, "no /api/v1/users/{id} in the document")
	params := pathItem["get"].(map[string]any)["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
	assert.Equal(t, "path", params[0].(map[string]any)["in"])
	assert.Equal(t, http.StatusNoContent, recServe(t, r, http.MethodGet, "/api/v1/users/7").Code)
	assert.NoError(t, specout.Check(d, specoutchi.New(r)))
}

func TestRecBinderPlainMountLeavesOutThePrefix(t *testing.T) {
	d, r, sub := adaptertest.Generator(), chi.NewRouter(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(sub)).Get("/x", recNC(recHit204))
	r.Mount("/api", sub)
	assert.Contains(t, adaptertest.Paths(t, d), "/x")
	assert.Equal(t, []string{"GET /api/x"}, adaptertest.Strays(t, specout.Check(d, specoutchi.New(r))))
}

func TestRecBinderCatchAllIsLeftOut(t *testing.T) {
	d, r := adaptertest.Generator(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/files/*", recNC(recHit204))
	assert.Empty(t, adaptertest.Paths(t, d))
	assert.NoError(t, specout.Check(d, specoutchi.New(r)))
}

func TestRecBinderPathValues(t *testing.T) {
	for _, tc := range []struct{ route, target, name, want string }{
		{"/users/{id}", "/users/42", "id", "42"},
		{"/users/{id:[0-9]+}", "/users/7", "id", "7"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, r := adaptertest.Generator(), chi.NewRouter()
			specout.Bind(d, specoutchi.New(r)).Get(tc.route, recNC(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Value", r.PathValue(tc.name))
				w.WriteHeader(http.StatusNoContent)
			}))
			w := recServe(t, r, http.MethodGet, tc.target)
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("X-Value"))
		})
	}
}

func TestRecBinderWithRunsMiddleware(t *testing.T) {
	d, r := adaptertest.Generator(), chi.NewRouter()
	var order []string
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "middleware")
			next.ServeHTTP(w, req)
		})
	}
	specout.Bind(d, specoutchi.New(r.With(mw))).Get("/x", recNC(func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusNoContent)
	}))
	assert.Equal(t, http.StatusNoContent, recServe(t, r, http.MethodGet, "/x").Code)
	assert.Equal(t, []string{"middleware", "handler"}, order)
	assert.Contains(t, adaptertest.Paths(t, d), "/x")
}

func TestRecBinderCheckReportsStrays(t *testing.T) {
	d, r := adaptertest.Generator(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/documented", recNC(recHit204))
	r.Get("/plain", recHit204)
	r.Get("/openapi.json", recHit204)
	r.Get("/debug/pprof", recHit204)
	r.Handle("/any", http.HandlerFunc(recHit204))
	got := adaptertest.Strays(t, specout.Check(d, specoutchi.New(r), "/openapi.json", "/debug/*"))
	assert.Contains(t, got, "GET /plain")
	assert.Contains(t, got, "GET /any")
	assert.NotContains(t, got, "GET /documented")
	assert.NotContains(t, got, "GET /openapi.json")
	assert.NotContains(t, got, "GET /debug/pprof")
}

func TestRecBinderNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specout.Bind(nil, specoutchi.New(chi.NewRouter())) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutchi.New(nil)) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutchi.New((*chi.Mux)(nil))) })
}
