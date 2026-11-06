package recorder_test

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyPassesOnFullCoverage(t *testing.T) {
	d, rec := statusRoute(t)
	for _, target := range []string{"/items", "/items?status=201", "/items?status=409"} {
		serve(rec, http.MethodPost, target)
	}
	wantNoErr(t, d, rec, "", "expected clean verify")
}

func TestVerifyFailsOnDeclaredButUnproduced(t *testing.T) {
	d, rec := statusRoute(t)
	serve(rec, http.MethodPost, "/items")
	require.NotEmpty(t, findErr(verify(d, rec), "never produced"), "expected never-produced failure")
}

func TestDeclaredDefaultNotDrift(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1",
		ErrorType: struct {
			Msg string `json:"msg"`
		}{},
		DefaultErrors: []int{400, 404, 500},
	})
	rec := stdRoute(d, "GET", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	serve(rec, "GET", "/x")
	wantNoErr(t, d, rec, "spec does not declare it", "declared default flagged as drift")
}

func TestHeadMatchesGet(t *testing.T) {
	d := gen()
	rec := stdRoute(d, "GET", hit204)
	serve(rec, "GET", "/x")
	serve(rec, "HEAD", "/x")
	wantNoErr(t, d, rec, "", "HEAD drift")
}

func TestDeclaredHeadNoDrift(t *testing.T) {
	d := gen()
	rec := stdRoute(d, "HEAD", hit204)
	serve(rec, "HEAD", "/x")
	wantNoErr(t, d, rec, "", "declared HEAD drift")
}

func TestUnmatchedRecordsNothing(t *testing.T) {
	d := gen()
	rec := stdRoute(d, "GET", hit204)
	serve(rec, "GET", "/nope")
	wantNoErr(t, d, rec, "404", "404 keyed as drift")
}

func TestSpecEndpointSkipped(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(mux)).Handle("GET", "/x", nc(hit204))
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := recorder.New(mux, "/openapi.json")
	serve(rec, "GET", "/openapi.json")
	wantNoErr(t, d, rec, "openapi.json", "spec endpoint flagged as drift")
}

func TestSubtreeMountNotKeyed(t *testing.T) {
	d := gen()
	rec := stdRoute(d, "GET", hit204)
	outer := http.NewServeMux()
	outer.Handle("/api/v3/", http.StripPrefix("/api/v3", rec))
	for _, c := range []struct{ method, target string }{
		{"GET", "/api/v3/nope"},
		{"HEAD", "/api/v3/x"},
	} {
		outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), c.method, c.target, nil))
	}
	wantNoErr(t, d, rec, "api/v3", "mount keyed as drift")
}

func TestStdMuxPatternCaptured(t *testing.T) {
	mux := http.NewServeMux()
	d := gen()
	specout.Bind(d, specoutstd.New(mux)).Handle("GET", "/items/{id}", nc(hit204))
	rec := recorder.New(mux)
	serve(rec, "GET", "/items/9")
	wantNoErr(t, d, rec, "", "verify errors")
}

func TestStdExactPatternsAreRecorded(t *testing.T) {
	for _, path := range []string{"/", "/items/"} {
		t.Run(path, func(t *testing.T) {
			d, r := gen(), http.NewServeMux()
			specout.Bind(d, specoutstd.New(r)).Handle("GET", path+"{$}", nc(hit204))
			rec := recorder.New(r)
			serve(rec, http.MethodGet, path)
			assert.Empty(t, verify(d, rec))
		})
	}
}

func TestStdExactAndSubtreeAreTwoRoutes(t *testing.T) {
	d, r := gen(), http.NewServeMux()
	std := specout.Bind(d, specoutstd.New(r))
	std.Handle("GET", "/a/{$}", nc(hit204))
	std.Handle("GET", "/a/", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
		Responses:   []specout.Response{{Status: http.StatusAccepted}},
	})
	rec := recorder.New(r)
	serve(rec, http.MethodGet, "/a/")
	serve(rec, http.MethodGet, "/a/b")
	assert.Empty(t, verify(d, rec))
	assert.Equal(t, []string{"/a/"}, slices.Collect(maps.Keys(adaptertest.Paths(t, d))), "the subtree is not in the document")
}

func TestStdSkipsTheMuxPattern(t *testing.T) {
	for _, skips := range [][]string{nil, {"/a/"}} {
		d, r := gen(), http.NewServeMux()
		specout.Bind(d, specoutstd.New(r)).Handle("GET", "/a/{$}", nc(hit204))
		r.HandleFunc("GET /a/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
		rec := recorder.New(r, skips...)
		serve(rec, http.MethodGet, "/a/")
		serve(rec, http.MethodGet, "/a/b")
		assert.Equal(t, skips == nil, findErr(verify(d, rec), "418") != "", "skips %v", skips)
	}
}

func TestSlashRoutesKeepSeparateCoverage(t *testing.T) {
	d, r := gen(), http.NewServeMux()
	std := specout.Bind(d, specoutstd.New(r))
	std.Handle("GET", "/api/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204, OperationID: "plain"})
	std.Handle("GET", "/api/items/", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
		OperationID: "slash",
		Responses:   []specout.Response{{Status: http.StatusAccepted}},
	})
	rec := recorder.New(r)
	serve(rec, http.MethodGet, "/api/items/")
	assert.Contains(t, findErr(verify(d, rec), "never produced"), "GET /api/items 204")
	serve(rec, http.MethodGet, "/api/items")
	assert.Empty(t, verify(d, rec))
	statuses, err := bridge.Statuses(d, false)
	require.NoError(t, err)
	assert.Len(t, statuses, 2)
}

func TestExplicitHeadAndGetNeedSeparateCoverage(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			d, r := gen(), http.NewServeMux()
			specout.Bind(d, specoutstd.New(r)).Handle("GET", "/x", nc(hit204))
			specout.Bind(d, specoutstd.New(r)).Handle("HEAD", "/x", nc(hit204))
			rec := recorder.New(r)
			serve(rec, method, "/x")
			assert.NotEmpty(t, findErr(verify(d, rec), "never produced"))
			serve(rec, http.MethodGet, "/x")
			serve(rec, http.MethodHead, "/x")
			assert.Empty(t, verify(d, rec))
		})
	}
}

func TestImplicitHeadCountsForGetHandler(t *testing.T) {
	d, r := gen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(r)).Handle("GET", "/x", nc(hit204))
	rec := recorder.New(r)
	serve(rec, http.MethodHead, "/x")
	assert.Empty(t, verify(d, rec))
}

func TestVerifyMatchesARouterWithoutPatterns(t *testing.T) {
	d := gen()
	d.Document(http.MethodGet, "/x", nc(hit204))
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x" {
			http.NotFound(w, r)
			return
		}
		hit204(w, r)
	})
	rec := recorder.New(router)
	serve(rec, http.MethodGet, "/x")
	serve(rec, http.MethodGet, "/missing")
	assert.Empty(t, verify(d, rec))

	rec = recorder.New(http.StripPrefix("/api", router))
	serve(rec, http.MethodGet, "/api/x")
	assert.NotEmpty(t, findErr(verify(d, rec), "recorder matched no route in 1 requests"))
}

func TestObserveFeedsVerify(t *testing.T) {
	d := gen()
	d.Document(http.MethodGet, "/items/{id}", nc(hit204))
	rec := recorder.New(nil, "/openapi.json")
	bridge.Observe(rec, http.MethodGet, "/items/{id}", http.StatusNoContent)
	bridge.Observe(rec, http.MethodGet, "/openapi.json", http.StatusOK)
	assert.Empty(t, verify(d, rec))
	assert.Panics(t, func() { serve(rec, http.MethodGet, "/items/1") })
}

func TestRouterRedirectsAreNotRouteResponses(t *testing.T) {
	for _, tc := range []struct{ name, target string }{
		{"clean", "/a/../items"},
		{"slash", "/dir"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, r := gen(), http.NewServeMux()
			specout.Bind(d, specoutstd.New(r)).Handle("GET", "/items", nc(hit204))
			specout.Bind(d, specoutstd.New(r)).Handle("GET", "/dir/", nc(hit204))
			rec := recorder.New(r)
			w := httptest.NewRecorder()
			rec.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, http.NoBody))
			require.Contains(t, []int{http.StatusMovedPermanently, http.StatusTemporaryRedirect}, w.Code)
			assert.Empty(t, findErr(verify(d, rec), "does not declare"))
		})
	}
}

type userKey struct{}

func withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, "ada")))
	})
}

func TestVerifyMatchesRequestsBehindOuterMiddleware(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	routes := specout.Bind(d, specoutstd.New(mux))
	routes.Handle("GET", "/tasks/{id}", specout.Handler[struct{}, responseBody]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Responses:   []specout.Response{{Status: http.StatusOK}, {Status: http.StatusUnauthorized}},
	})
	routes.Handle("GET", "/tasks/new", nc(hit204))
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

type discard struct{}

func (discard) Header() http.Header { return http.Header{} }

func (discard) Write(b []byte) (int, error) { return len(b), nil }

func (discard) WriteHeader(int) {}

func TestRawResponsesCannotBypassVerification(t *testing.T) {
	d, mux := gen(), http.NewServeMux()
	specout.Bind(d, specoutstd.New(mux)).Handle("GET", "/x", specout.Handler[struct{}, string]{
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
