package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdExactPatternsAreRecorded(t *testing.T) {
	for _, path := range []string{"/", "/items/"} {
		t.Run(path, func(t *testing.T) {
			d, r := gen(), http.NewServeMux()
			specout.Std(d, r).Get(path+"{$}", nc(hit204))
			rec := recorder.New(r)
			serve(rec, http.MethodGet, path)
			assert.Empty(t, verify(d, rec))
		})
	}
}

func TestSlashRoutesKeepSeparateCoverage(t *testing.T) {
	for _, adapter := range []string{"std", "chi", "gorilla"} {
		t.Run(adapter, func(t *testing.T) {
			d := gen()
			var router http.Handler
			var register func(string, specout.Handler[struct{}, specout.NoContent])
			switch adapter {
			case "chi":
				r := chi.NewRouter()
				sub := chi.NewRouter()
				r.Mount("/api", sub)
				register = specout.Chi(d, sub).Get[struct{}, specout.NoContent]
				router = r
			case "gorilla":
				r := mux.NewRouter()
				register = specout.Gorilla(d, r.PathPrefix("/api").Subrouter()).Get[struct{}, specout.NoContent]
				router = r
			case "std":
				r := http.NewServeMux()
				register = func(path string, h specout.Handler[struct{}, specout.NoContent]) {
					specout.Std(d, r).Get("/api"+path, h)
				}
				router = r
			}
			register("/items", nc(hit204).WithOperationID("plain"))
			register("/items/", nc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}).WithOperationID("slash").WithResponse(specout.Response{Status: http.StatusAccepted}))
			if r, ok := router.(chi.Router); ok {
				require.NoError(t, specout.Chi(d, r).Adopt())
			}
			rec := recorder.New(router)
			serve(rec, http.MethodGet, "/api/items/")
			assert.Contains(t, findErr(verify(d, rec), "never produced"), "GET /api/items 204")
			serve(rec, http.MethodGet, "/api/items")
			assert.Empty(t, verify(d, rec))
			statuses, err := d.DeclaredStatuses()
			require.NoError(t, err)
			assert.Len(t, statuses, 2)
		})
	}
}

func TestChiEncodedSlashIsRecorded(t *testing.T) {
	d, r := gen(), chi.NewRouter()
	specout.Chi(d, r).Get("/items/{id}", nc(hit204))
	require.NoError(t, specout.Chi(d, r).Adopt())
	rec := recorder.New(r)
	serve(rec, http.MethodGet, "/items/a%2Fb")
	assert.Empty(t, verify(d, rec))
}

func TestExplicitHeadAndGetNeedSeparateCoverage(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			d, r := gen(), http.NewServeMux()
			specout.Std(d, r).Get("/x", nc(hit204))
			specout.Std(d, r).Head("/x", nc(hit204))
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
	specout.Std(d, r).Get("/x", nc(hit204))
	rec := recorder.New(r)
	serve(rec, http.MethodHead, "/x")
	assert.Empty(t, verify(d, rec))
}

func TestChiMountRootRouteRecordsTheDocumentedPath(t *testing.T) {
	d, r := gen(), chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		specout.Chi(d, r).Get("/", nc(hit204))
	})
	require.NoError(t, specout.Chi(d, r).Adopt())
	for _, target := range []string{"/users", "/users/"} {
		t.Run(target, func(t *testing.T) {
			rec := recorder.New(r)
			serve(rec, http.MethodGet, target)
			assert.Empty(t, verify(d, rec))
		})
	}
}

func TestChiUnmatchedMountStubIsNotAnOperation(t *testing.T) {
	d, r := gen(), chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		specout.Chi(d, r).Get("/x", nc(hit204))
	})
	require.NoError(t, specout.Chi(d, r).Adopt())
	rec := recorder.New(r)
	serve(rec, http.MethodGet, "/api")
	serve(rec, http.MethodGet, "/api/x")
	assert.Empty(t, verify(d, rec))
}

func TestVerifyNamesARouterHiddenByMiddleware(t *testing.T) {
	d, r := gen(), chi.NewRouter()
	b := specout.Chi(d, r)
	b.Get("/x", nc(hit204))
	require.NoError(t, b.Adopt())
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { r.ServeHTTP(w, req) })
	rec := recorder.New(wrapped)
	serve(rec, http.MethodGet, "/x")
	assert.NotEmpty(t, findErr(verify(d, rec), "recorder matched no route in 1 requests"))
}

func TestObserveFeedsVerify(t *testing.T) {
	d := gen()
	specout.Document(d, http.MethodGet, "/items/{id}", nc(hit204))
	rec := recorder.New(nil, specout.Skip("/openapi.json"))
	rec.Observe(http.MethodGet, "/items/{id}", http.StatusNoContent)
	rec.Observe(http.MethodGet, "/openapi.json", http.StatusOK)
	assert.Empty(t, verify(d, rec))
	assert.Panics(t, func() { serve(rec, http.MethodGet, "/items/1") })
}

// A redirect the router makes, to the cleaned path or to the path with its
// trailing slash added, is not the route's response.
func TestRouterRedirectsAreNotRouteResponses(t *testing.T) {
	for _, tc := range []struct{ name, target string }{
		{"std clean", "/a/../items"},
		{"std slash", "/dir"},
		{"gorilla clean", "/a/../items"},
		{"gorilla strict slash", "/dir"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := gen()
			var router http.Handler
			if strings.HasPrefix(tc.name, "std") {
				r := http.NewServeMux()
				specout.Std(d, r).Get("/items", nc(hit204))
				specout.Std(d, r).Get("/dir/", nc(hit204))
				router = r
			} else {
				r := mux.NewRouter().StrictSlash(true)
				specout.Gorilla(d, r).Get("/items", nc(hit204))
				specout.Gorilla(d, r).Get("/dir/", nc(hit204))
				router = r
			}
			rec := recorder.New(router)
			w := httptest.NewRecorder()
			rec.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, http.NoBody))
			require.Contains(t, []int{http.StatusMovedPermanently, http.StatusTemporaryRedirect}, w.Code)
			assert.Empty(t, findErr(verify(d, rec), "does not declare"))
		})
	}
}
