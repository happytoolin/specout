package specout_test

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
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

// panicValue runs fn and returns what it panicked with, or nil.
func panicValue(fn func()) any {
	var v any
	func() {
		defer func() { v = recover() }()
		fn()
	}()
	return v
}

func TestChiRootMountInsideGroupIsDocumented(t *testing.T) {
	d, root := newGen(), chi.NewRouter()
	root.Route("/v1", func(r chi.Router) {
		api := chi.NewRouter()
		specout.Chi(d, api).Get("/x", okGet)
		r.Mount("/", api)
	})
	adopt(t, specout.Chi(d, root))
	assert.Contains(t, docPaths(t, d), "/v1/x")
}

func TestReplacedChiRouteNamesTheCause(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	b := specout.Chi(d, r)
	b.Get("/x", okGet)
	b.Get("/x", okGet)
	adopt(t, b)
	wantBuildErr(t, d, "is not served by any adopted router")
}

func TestGorillaBadTemplatePanicsWithGorillaReason(t *testing.T) {
	for _, pattern := range []string{"/items/{id", "items"} {
		t.Run(pattern, func(t *testing.T) {
			d := newGen()
			msg := fmt.Sprint(panicValue(func() { specout.Gorilla(d, mux.NewRouter()).Get(pattern, okGet) }))
			assert.Contains(t, msg, "specout: gorilla route GET "+pattern)
			assert.NotContains(t, docPaths(t, d), pattern)
		})
	}
}

func TestGorillaPathlessRouteIsSkippable(t *testing.T) {
	d, r := newGen(), mux.NewRouter()
	specout.Gorilla(d, r).Get("/x", okGet)
	r.Methods(http.MethodOptions).HandlerFunc(okBody)
	var stray *specout.StrayError
	require.ErrorAs(t, specout.Gorilla(d, r).Adopt(), &stray)
	assert.Equal(t, []string{"OPTIONS *"}, stray.Routes)
	require.NoError(t, specout.Gorilla(d, r).Adopt(specout.Skip("*")))
}

func TestGorillaMountedRouterIsNotAStray(t *testing.T) {
	d, root, api := newGen(), mux.NewRouter(), mux.NewRouter()
	specout.Gorilla(d, api).Get("/api/x", okGet)
	root.PathPrefix("/api").Handler(api)
	require.NoError(t, specout.Gorilla(d, root).Adopt())
}

func TestGorillaSlashRegexIsCatchAll(t *testing.T) {
	d, r := newGen(), mux.NewRouter()
	b := specout.Gorilla(d, r)
	b.Get("/files/{path:.*}", okGet)
	b.Get("/blobs/{key:[a-z0-9/]+}", okGet)
	b.Get("/items/{id:[0-9]+}", okGet)
	b.Get("/names/{name:[^/]+}", okGet)
	paths := docPaths(t, d)
	assert.ElementsMatch(t, []string{"/items/{id}", "/names/{name}"}, keys(paths))
	require.NoError(t, b.Adopt())
}

func TestDocumentRejectsNonOpenAPIPaths(t *testing.T) {
	for pattern, want := range map[string]string{
		"/items/:id": "uses router syntax :id; write the parameter as {id}",
		"/items/{id": "{ without a matching }",
		"/items/id}": "} without a matching {",
		"/a//b":      "drop {$} and empty segments",
		"/c/{$}":     "drop {$} and empty segments",
		"items/{id}": "must start with /",
	} {
		t.Run(pattern, func(t *testing.T) {
			msg := fmt.Sprint(panicValue(func() { specout.Document(newGen(), http.MethodGet, pattern, okGet) }))
			assert.Contains(t, msg, want)
		})
	}
	// a colon inside a segment is a literal, as in AIP custom methods
	assert.Contains(t, docOf(t, http.MethodPost, "/items/{id}:cancel", okGet)["paths"], "/items/{id}:cancel")
}

func TestBindRecordsOnlyAfterTheRouterAccepts(t *testing.T) {
	d := newGen()
	require.Panics(t, func() {
		specout.Bind(d, http.MethodGet, "/boom", okGet, func(http.Handler) string { panic("router said no") })
	})
	msg := fmt.Sprint(panicValue(func() {
		specout.Bind(d, http.MethodGet, "items", okGet, func(http.Handler) string { return "items" })
	}))
	assert.Contains(t, msg, "mounted at items: the path must start with /")
	specout.Bind(d, http.MethodGet, "/ok/:id", okGet, func(next http.Handler) string {
		require.NotNil(t, next)
		return "/ok/{id}"
	})
	assert.Equal(t, []string{"/ok/{id}"}, keys(docPaths(t, d)))

	called := false
	require.Panics(t, func() {
		specout.Bind(d, http.MethodGet, "/late", okGet, func(http.Handler) string { called = true; return "/late" })
	})
	assert.False(t, called, "a frozen generator must not touch the router")
}

func TestBindRejectsADuplicateRoute(t *testing.T) {
	d := newGen()
	mounts := 0
	mount := func(http.Handler) string { mounts++; return "/items/{id}" }
	specout.Bind(d, http.MethodGet, "/items/:id", okGet, mount)
	specout.Bind(d, http.MethodPost, "/items/:id", okGet, mount)
	msg := fmt.Sprint(panicValue(func() { specout.Bind(d, http.MethodGet, "/items/:key", okGet, mount) }))
	assert.Contains(t, msg, `specout: duplicate route GET /items/{id} (registered as "/items/:id" and "/items/:key")`)
	assert.Equal(t, 3, mounts)
	assert.Len(t, docPaths(t, d)["/items/{id}"], 2, "the duplicate left a record")
}

func TestCheckRoutesMatchesBindByMethodAndPath(t *testing.T) {
	d := newGen()
	specout.Bind(d, http.MethodGet, "/items/:id", okGet, func(http.Handler) string { return "/items/{id}" })
	specout.Document(d, http.MethodPost, "/items", okGet)
	err := d.CheckRoutes([]specout.RouteKey{
		{Method: http.MethodGet, Path: "/items/{id}"},
		{Method: http.MethodPost, Path: "/items"},
		{Method: http.MethodDelete, Path: "/items/{id}"},
		{Method: http.MethodDelete, Path: "/items/{id}"},
		{Method: "", Path: "/any"},
		{Method: http.MethodGet, Path: "/openapi.json"},
	}, specout.Skip("/openapi.json"))
	var stray *specout.StrayError
	require.ErrorAs(t, err, &stray)
	assert.Equal(t, []string{"DELETE /items/{id}", "? /any (no method constraint)"}, stray.Routes)
	require.NoError(t, d.CheckRoutes(nil))
}

func TestBindersRejectNilArguments(t *testing.T) {
	for name, fn := range map[string]func(){
		"chi generator":     func() { specout.Chi(nil, chi.NewRouter()) },
		"chi router":        func() { specout.Chi(newGen(), nil) },
		"gorilla generator": func() { specout.Gorilla(nil, mux.NewRouter()) },
		"gorilla router":    func() { specout.Gorilla(newGen(), nil) },
		"std generator":     func() { specout.Std(nil, http.NewServeMux()) },
		"std mux":           func() { specout.Std(newGen(), nil) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, fmt.Sprint(panicValue(fn)), "needs a generator and")
		})
	}
}

// An Adopt on a mounted chi router documents /x where clients call /api/x. A
// spec request through the outer router shows the mount and fails the build,
// unless a server URL declares the prefix.
func TestChiInnerAdoptFailsUnderUndeclaredMount(t *testing.T) {
	for _, tc := range []struct {
		servers []specout.Server
		want    int
	}{
		{nil, http.StatusInternalServerError},
		{[]specout.Server{{URL: "https://example.com/api/"}}, http.StatusOK},
		{[]specout.Server{{URL: "/v1"}}, http.StatusInternalServerError},
		{[]specout.Server{{URL: "{scheme}://example.com"}}, http.StatusOK},
	} {
		captureLog(t)
		d := specout.New(specout.Config{Title: "t", Version: "1", Servers: tc.servers})
		root, api := chi.NewRouter(), chi.NewRouter()
		b := specout.Chi(d, api)
		b.Get("/x", okGet)
		require.NoError(t, b.Adopt())
		root.Mount("/api", api)
		root.Handle("/openapi.json", d)

		w := httptest.NewRecorder()
		root.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", http.NoBody))
		assert.Equal(t, tc.want, w.Code, "servers %v", tc.servers)
		if tc.want != http.StatusOK {
			wantBuildErr(t, d, "GET /x is served at /api/x, under a router mounted at /api")
		}
	}
}

// The outermost router passes the mount check, and so does a spec served on
// the adopted router itself.
func TestChiOuterAdoptPassesMountCheck(t *testing.T) {
	d := newGen()
	root := chi.NewRouter()
	root.Route("/api", func(r chi.Router) {
		specout.Chi(d, r).Get("/x", okGet)
	})
	root.Handle("/openapi.json", d)
	require.NoError(t, specout.Chi(d, root).Adopt(specout.Skip("/openapi.json")))
	w := httptest.NewRecorder()
	root.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"/api/x"`)
}

// A std path that ends in / matches the whole subtree, as {path...} does, so
// both stay out of the paths object. {$} keeps the exact path.
func TestStdSubtreeIsACatchAll(t *testing.T) {
	d, sm := newGen(), http.NewServeMux()
	std := specout.Std(d, sm)
	std.Get("/files/", okGet)
	std.Get("/blobs/{path...}", okGet)
	std.Get("/dir/{$}", okGet)
	std.Get("/{$}", okGet)
	assert.ElementsMatch(t, []string{"/dir/", "/"}, slices.Collect(maps.Keys(docPaths(t, d))))
	declared := declaredStatuses(t, d)
	assert.Contains(t, declared, specout.RouteKey{Method: http.MethodGet, Path: "/files/"}, "the subtree keeps its drift entry")
}
