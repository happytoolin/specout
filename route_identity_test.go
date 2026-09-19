package specout_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:noinline
func routeFactory(summary string) noBody {
	return noBody{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		Summary: summary,
	}
}

func TestChiFactoryRoutesKeepMetadata(t *testing.T) {
	d, root := newGen(), chi.NewRouter()
	for _, prefix := range []string{"/b", "/a"} {
		root.Route(prefix, func(r chi.Router) {
			specout.Chi(d, r).Get("/x", routeFactory(prefix))
		})
	}
	adopt(t, specout.Chi(d, root))
	doc := buildDoc(t, d)
	for _, prefix := range []string{"/a", "/b"} {
		assert.Equal(t, prefix, opOf(t, doc, prefix+"/x", "get")["summary"])
	}
}

func TestChiIdentitySurvivesMiddleware(t *testing.T) {
	d, root := newGen(), chi.NewRouter()
	wrapped := root.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req)
		})
	})
	specout.Chi(d, wrapped).Get("/api/x", routeFactory("wrapped"))
	adopt(t, specout.Chi(d, root))
	assert.Equal(t, "wrapped", opOf(t, buildDoc(t, d), "/api/x", "get")["summary"])
	w := httptest.NewRecorder()
	root.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/x", nil))
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestStatusViewRewalksAfterLateRegistration(t *testing.T) {
	d, root := newGen(), chi.NewRouter()
	routes := specout.Chi(d, root)
	routes.Get("/early", okGet)
	adopt(t, routes)
	require.Contains(t, declaredStatuses(t, d), specout.RouteKey{Method: http.MethodGet, Path: "/early"})
	routes.Get("/late", okGet)
	statuses := declaredStatuses(t, d)
	require.Contains(t, statuses, specout.RouteKey{Method: http.MethodGet, Path: "/early"})
	require.Contains(t, statuses, specout.RouteKey{Method: http.MethodGet, Path: "/late"})
}

type routeValueHandler struct{}

func (routeValueHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func TestAdoptReportsStructHandler(t *testing.T) {
	for name, setup := range map[string]func(*specout.Generator) error{
		"chi": func(d *specout.Generator) error {
			r := chi.NewRouter()
			r.Method(http.MethodGet, "/x", routeValueHandler{})
			return specout.Chi(d, r).Adopt()
		},
		"gorilla": func(d *specout.Generator) error {
			r := mux.NewRouter()
			r.Handle("/x", routeValueHandler{}).Methods(http.MethodGet)
			return specout.Gorilla(d, r).Adopt()
		},
	} {
		t.Run(name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() { err = setup(newGen()) })
			var stray *specout.StrayError
			require.ErrorAs(t, err, &stray)
			assert.Contains(t, stray.Routes, "GET /x")
		})
	}
}

func TestDocumentMatchesMethodAndPath(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	h := routeFactory("manual")
	r.Get("/documented", h.HandlerFunc)
	r.Get("/stray", h.HandlerFunc)
	r.Post("/documented", h.HandlerFunc)
	specout.Document(d, http.MethodGet, "/documented", h)
	err := specout.Chi(d, r).Adopt()
	var stray *specout.StrayError
	require.ErrorAs(t, err, &stray)
	assert.ElementsMatch(t, []string{"GET /stray", "POST /documented"}, stray.Routes)
}

func TestEquivalentTemplatePathsFail(t *testing.T) {
	for _, methods := range [][2]string{
		{http.MethodGet, http.MethodGet},
		{http.MethodGet, http.MethodPost},
	} {
		t.Run(methods[0]+"_"+methods[1], func(t *testing.T) {
			d := newGen()
			specout.Document(d, methods[0], "/x/{id}", okGet)
			specout.Document(d, methods[1], "/x/{name}", okGet)
			wantBuildErr(t, d, "equivalent templated paths /x/{id} and /x/{name}")
		})
	}
}

func TestSameTemplateAllowsDifferentMethods(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x/{id}", okGet)
	specout.Document(d, http.MethodPost, "/x/{id}", okGet)
	item := docPaths(t, d)["/x/{id}"].(map[string]any)
	assert.Contains(t, item, "get")
	assert.Contains(t, item, "post")
}

func TestEquivalentCatchAllsRemainExcluded(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/files/{path...}", okGet)
	specout.Document(d, http.MethodPost, "/files/{rest...}", okGet)
	paths := docPaths(t, d)
	assert.NotContains(t, paths, "/files/{path...}")
	assert.NotContains(t, paths, "/files/{rest...}")
}

func TestDuplicateCatchAllMethodStillFails(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/files/{path...}", okGet)
	specout.Document(d, http.MethodGet, "/files/{path...}", okGet)
	wantBuildErr(t, d, "duplicate route")
}

func TestRegistrationFailureDoesNotMutateRouter(t *testing.T) {
	tests := map[string]struct {
		newRouter func() http.Handler
		register  func(*specout.Generator, http.Handler, noBody)
	}{
		"chi": {
			func() http.Handler { return chi.NewRouter() },
			func(d *specout.Generator, r http.Handler, h noBody) {
				specout.Chi(d, r.(*chi.Mux)).Get("/late", h)
			},
		},
		"gorilla": {
			func() http.Handler { return mux.NewRouter() },
			func(d *specout.Generator, r http.Handler, h noBody) {
				specout.Gorilla(d, r.(*mux.Router)).Get("/late", h)
			},
		},
		"std": {
			func() http.Handler { return http.NewServeMux() },
			func(d *specout.Generator, r http.Handler, h noBody) {
				specout.Std(d, r.(*http.ServeMux)).Get("/late", h)
			},
		},
	}
	for name, tc := range tests {
		for failure, h := range map[string]noBody{"nil": {}, "frozen": okGet} {
			t.Run(name+"/"+failure, func(t *testing.T) {
				d, router := newGen(), tc.newRouter()
				if failure == "frozen" {
					require.NotNil(t, buildDoc(t, d))
				}
				require.Panics(t, func() { tc.register(d, router, h) })
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/late", nil))
				assert.Equal(t, http.StatusNotFound, w.Code)
			})
		}
	}
}
