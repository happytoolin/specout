package specoutchi_test

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutchi"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type routeNoBody = specout.Handler[struct{}, specout.NoContent]

func routeOKBody(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

var routeOK = routeNoBody{HandlerFunc: routeOKBody}

func routeGen() *specout.Generator { return adaptertest.Generator() }

func routeKeys(t *testing.T, d *specout.Generator) []string {
	t.Helper()
	return slices.Collect(maps.Keys(adaptertest.Paths(t, d)))
}

func routeBuildErr(t *testing.T, d *specout.Generator, want string) {
	t.Helper()
	_, err := d.Build()
	require.ErrorContains(t, err, want)
}

func routeStatuses(t *testing.T, d *specout.Generator) map[bridge.Key]map[int]bool {
	t.Helper()
	st, err := bridge.Statuses(d, false)
	require.NoError(t, err)
	return st
}

func routeServe(t *testing.T, h http.Handler, method, target string) int {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return w.Code
}

func routeGet(t *testing.T, d *specout.Generator, path string) map[string]any {
	t.Helper()
	item, ok := adaptertest.Paths(t, d)[path].(map[string]any)
	require.True(t, ok, "no path %s in the document", path)
	op, ok := item["get"].(map[string]any)
	require.True(t, ok, "no GET %s in the document", path)
	return op
}

func routePanic(f func()) string {
	var v any
	func() {
		defer func() { v = recover() }()
		f()
	}()
	return fmt.Sprint(v)
}

func TestRegexAsteriskRouteIsDocumented(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/items/{id:[0-9]*}", routeOK)
	require.Equal(t, http.StatusNoContent, routeServe(t, r, http.MethodGet, "/items/123"))
	assert.Contains(t, adaptertest.Paths(t, d), "/items/{id}")
}

type routeUncomparable struct {
	*chi.Mux

	_ []int
}

func TestCheckAcceptsUncomparableRouter(t *testing.T) {
	d, r := routeGen(), routeUncomparable{Mux: chi.NewRouter()}
	specout.Bind(d, specoutchi.New(r)).Get("/x", routeOK)
	other := routeUncomparable{Mux: chi.NewRouter()}
	specout.Bind(d, specoutchi.New(other)).Post("/y", routeOK)
	require.NotPanics(t, func() {
		require.NoError(t, specout.Check(d, specoutchi.New(r)))
		require.NoError(t, specout.Check(d, specoutchi.New(r)))
		require.NoError(t, specout.Check(d, specoutchi.New(other)))
	})
	assert.ElementsMatch(t, []string{"/x", "/y"}, routeKeys(t, d))
}

func TestStatusesSeeLateRegistration(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	routes := specout.Bind(d, specoutchi.New(r))
	routes.Get("/early", routeOK)
	require.Contains(t, routeStatuses(t, d), bridge.Key{Method: http.MethodGet, Path: "/early"})
	routes.Get("/late", routeOK)
	statuses := routeStatuses(t, d)
	require.Contains(t, statuses, bridge.Key{Method: http.MethodGet, Path: "/early"})
	require.Contains(t, statuses, bridge.Key{Method: http.MethodGet, Path: "/late"})
	assert.Contains(t, adaptertest.Paths(t, d), "/late", "late route missing")
}

func TestSlashVariantsAreDistinct(t *testing.T) {
	d := routeGen()
	routes := specout.Bind(d, specoutchi.New(chi.NewRouter()))
	routes.Get("/a", routeOK)
	routes.Get("/a/", routeOK)
	routeBuildErr(t, d, "operationId")
}

func TestTrailingStarIsACatchAll(t *testing.T) {
	type catchAllResponse struct {
		Value string `json:"value"`
	}
	d := routeGen()
	specout.Bind(d, specoutchi.New(chi.NewRouter())).Get("/files/*", specout.Handler[struct{}, catchAllResponse]{HandlerFunc: routeOKBody})
	spec, err := d.Build()
	require.NoError(t, err)
	assert.NotContains(t, string(spec), "/files/", "catch-all leaked into paths")
	assert.NotContains(t, string(spec), "components", "catch-all leaked an unused response schema")
	assert.Contains(t, routeStatuses(t, d), bridge.Key{Method: http.MethodGet, Path: "/files/{*...}"}, "catch-all missing from drift keys")
}

func TestCheckReportsStraysAndKeepsDocumentedRoutes(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/ok", routeOK)
	r.Get("/stray", routeOKBody)
	require.ErrorContains(t, specout.Check(d, specoutchi.New(r)), "/stray")
	require.Contains(t, adaptertest.Paths(t, d), "/ok", "documented route lost to the stray")
}

func TestRouteGroupResolvesToTheFullPath(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	r.Route("/onboarding", func(cr chi.Router) {
		r := specout.Bind(d, specoutchi.New(cr, "/onboarding"))
		r.Get("/", routeOK)
	})
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
	assert.Contains(t, adaptertest.Paths(t, d), "/onboarding/", "group route not resolved to full path")
}

func TestStdAndChiShareAGenerator(t *testing.T) {
	d := routeGen()
	specout.Bind(d, specoutstd.New(http.NewServeMux())).Handle("GET", "/std", routeOK)
	specout.Bind(d, specoutchi.New(chi.NewRouter())).Get("/chi", routeOK)
	assert.ElementsMatch(t, []string{"/std", "/chi"}, routeKeys(t, d))
}

func TestBraceQuantifierParamNormalized(t *testing.T) {
	d := routeGen()
	specout.Bind(d, specoutchi.New(chi.NewRouter())).Get("/x/{id:[0-9]{4}}", routeOK)
	assert.Equal(t, "getXId", routeGet(t, d, "/x/{id}")["operationId"])
}

func TestBraceInParameterNameFails(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/x/{a{b}}", routeOK)
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
	routeBuildErr(t, d, "a path parameter name has a brace")
}

func TestRepeatedPathParameterAcrossRouteFails(t *testing.T) {
	d := routeGen()
	chi.NewRouter().Route("/a/{id}", func(cr chi.Router) {
		r := specout.Bind(d, specoutchi.New(cr, "/a/{id}"))
		r.Get("/b/{id}", routeOK)
	})
	routeBuildErr(t, d, `path parameter "id" appears twice`)
}

func TestSharedHandlerTwoGroups(t *testing.T) {
	h := specout.Handler[struct{}, struct{ V string }]{HandlerFunc: routeOKBody, Summary: "shared"}
	d, r := routeGen(), chi.NewRouter()
	for _, group := range []string{"/a", "/b"} {
		r.Route(group, func(cr chi.Router) { specout.Bind(d, specoutchi.New(cr, group)).Get("/x", h) })
	}
	r.Handle("/openapi.json", http.NotFoundHandler())
	require.NoError(t, specout.Check(d, specoutchi.New(r), "/openapi.json"))
	for _, p := range []string{"/a/x", "/b/x"} {
		assert.Equal(t, "shared", routeGet(t, d, p)["summary"], p)
	}
}

func TestChiGroups(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	var ran []string
	mark := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ran = append(ran, name)
				next.ServeHTTP(w, req)
			})
		}
	}
	r.Route("/tasks", func(cr chi.Router) {
		specout.Bind(d, specoutchi.New(cr, "/tasks")).Get("/", routeOK)
		cr.Group(func(g chi.Router) {
			specout.Bind(d, specoutchi.New(g.With(mark("auth")), "/tasks")).Delete("/{id}", routeOK)
		})
	})
	assert.ElementsMatch(t, []string{"/tasks/", "/tasks/{id}"}, routeKeys(t, d))
	assert.Equal(t, http.StatusNoContent, routeServe(t, r, http.MethodDelete, "/tasks/7"))
	assert.Equal(t, []string{"auth"}, ran, "With adds the middleware to the route")
}

//go:noinline
func routeFactory(summary string) routeNoBody {
	return routeNoBody{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		Summary: summary,
	}
}

func TestFactoryRoutesKeepMetadata(t *testing.T) {
	d := routeGen()
	r := chi.NewRouter()
	for _, prefix := range []string{"/b", "/a"} {
		r.Route(prefix, func(cr chi.Router) { specout.Bind(d, specoutchi.New(cr, prefix)).Get("/x", routeFactory(prefix)) })
	}
	for _, prefix := range []string{"/a", "/b"} {
		assert.Equal(t, prefix, routeGet(t, d, prefix+"/x")["summary"])
	}
}

func TestRouteOnAMiddlewareRouterIsDocumented(t *testing.T) {
	d, root := routeGen(), chi.NewRouter()
	wrapped := root.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req)
		})
	})
	specout.Bind(d, specoutchi.New(wrapped)).Get("/api/x", routeFactory("wrapped"))
	require.NoError(t, specout.Check(d, specoutchi.New(root)))
	assert.Equal(t, "wrapped", routeGet(t, d, "/api/x")["summary"])
	assert.Equal(t, http.StatusNoContent, routeServe(t, root, http.MethodGet, "/api/x"))
}

type routeValueHandler struct{}

func (routeValueHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func TestCheckReportsStructHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Method(http.MethodGet, "/x", routeValueHandler{})
	var err error
	require.NotPanics(t, func() { err = specout.Check(routeGen(), specoutchi.New(r)) })
	assert.Contains(t, adaptertest.Strays(t, err), "GET /x")
}

func TestCheckMatchesDocumentByMethodAndPath(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	h := routeFactory("manual")
	r.Get("/documented", h.HandlerFunc)
	r.Get("/stray", h.HandlerFunc)
	r.Post("/documented", h.HandlerFunc)
	d.Document(http.MethodGet, "/documented", h)
	assert.ElementsMatch(t, []string{"GET /stray", "POST /documented"}, adaptertest.Strays(t, specout.Check(d, specoutchi.New(r))))
}

func TestRegistrationFailureDoesNotMutateRouter(t *testing.T) {
	for failure, h := range map[string]routeNoBody{"nil": {}, "frozen": routeOK} {
		t.Run(failure, func(t *testing.T) {
			d, r := routeGen(), chi.NewRouter()
			if failure == "frozen" {
				_, err := d.Build()
				require.NoError(t, err)
			}
			require.Panics(t, func() { specout.Bind(d, specoutchi.New(r)).Get("/late", h) })
			assert.Equal(t, http.StatusNotFound, routeServe(t, r, http.MethodGet, "/late"))
		})
	}
}

func TestMountInsideRouteLeavesOutTheRoutePrefix(t *testing.T) {
	d, root := routeGen(), chi.NewRouter()
	root.Route("/v1", func(r chi.Router) {
		api := chi.NewRouter()
		specout.Bind(d, specoutchi.New(api)).Get("/x", routeOK)
		r.Mount("/", api)
	})
	assert.Contains(t, adaptertest.Paths(t, d), "/x")
	assert.Equal(t, []string{"GET /v1/x"}, adaptertest.Strays(t, specout.Check(d, specoutchi.New(root))))
}

func TestReplacedRouteNamesTheCause(t *testing.T) {
	routes := specout.Bind(routeGen(), specoutchi.New(chi.NewRouter()))
	routes.Get("/x", routeOK)
	assert.Contains(t, routePanic(func() { routes.Get("/x", routeOK) }), "duplicate route")
}

func TestEmptyAndDotSegmentsPanic(t *testing.T) {
	for _, p := range []string{"/a//b", "/a/./b", "/a/../b", "/a/..", "//a"} {
		t.Run(p, func(t *testing.T) {
			d := routeGen()
			msg := routePanic(func() { specout.Bind(d, specoutchi.New(chi.NewRouter())).Get(p, routeOK) })
			assert.Contains(t, msg, "specout: GET "+p+": mounted at "+p+": the path has")
			assert.Empty(t, adaptertest.Paths(t, d))
		})
	}
	d, r := routeGen(), chi.NewRouter()
	routes := specout.Bind(d, specoutchi.New(r))
	routes.Get("/", routeOK)
	routes.Get("/v/{n:[0-9]+\\.[0-9]+}", routeOK)
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
	assert.ElementsMatch(t, []string{"/", "/v/{n}"}, routeKeys(t, d))
}

func TestStarInParameterNameFails(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/a/{x*}", routeOK)
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
	routeBuildErr(t, d, `path parameter "x*" has a * in its name`)
}

func TestRegexAcrossSegmentsIsAParameter(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	routes := specout.Bind(d, specoutchi.New(r))
	routes.Get("/a/{x:.+}.json", routeOK)
	routes.Get("/b/{x:[a-z/]+}-v", routeOK)
	routes.Get("/c/{x:.+}", routeOK)
	routes.Get("/d/{x:.+}/e", routeOK)
	routes.Get("/f/{x:[^/]+}.json", routeOK)
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
	assert.ElementsMatch(t, []string{"/a/{x}.json", "/b/{x}-v", "/c/{x}", "/d/{x}/e", "/f/{x}.json"}, routeKeys(t, d))
}

func TestStrayRoutesAreSorted(t *testing.T) {
	for range 20 {
		d, r := routeGen(), chi.NewRouter()
		for _, method := range []string{http.MethodPut, http.MethodGet, http.MethodPost, http.MethodDelete} {
			r.Method(method, "/x", routeOK)
			r.Method(method, "/a", routeOK)
		}
		require.Equal(t, []string{
			"DELETE /a", "DELETE /x", "GET /a", "GET /x", "POST /a", "POST /x", "PUT /a", "PUT /x",
		}, adaptertest.Strays(t, specout.Check(d, specoutchi.New(r))))
	}
}

func TestRoutePrefixPassesCheck(t *testing.T) {
	d, root := routeGen(), chi.NewRouter()
	root.Route("/api", func(cr chi.Router) {
		r := specout.Bind(d, specoutchi.New(cr, "/api"))
		r.Get("/x", routeOK)
	})
	root.Handle("/openapi.json", http.NotFoundHandler())
	require.NoError(t, specout.Check(d, specoutchi.New(root), "/openapi.json"))
	assert.Contains(t, routeKeys(t, d), "/api/x")
}

func routeDocumented() (*specout.Generator, chi.Router) {
	d, r := routeGen(), chi.NewRouter()
	specout.Bind(d, specoutchi.New(r)).Get("/documented", specout.Handler[struct{}, struct{}]{HandlerFunc: routeOKBody})
	return d, r
}

func TestCheckPassesWhenEveryRouteIsDocumented(t *testing.T) {
	d, r := routeDocumented()
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
}

func TestCheckSkipsAPrefix(t *testing.T) {
	d, r := routeDocumented()
	r.Get("/debug/vars", routeOKBody)
	r.Get("/debug/hidden", routeOKBody)
	require.NoError(t, specout.Check(d, specoutchi.New(r), "/debug/*"), "skip must suppress /debug/*")
}

func TestCheckReportsAStray(t *testing.T) {
	d, r := routeDocumented()
	r.Get("/stray", routeOKBody)
	require.ErrorContains(t, specout.Check(d, specoutchi.New(r)), "/stray")
}

func TestTwoGeneratorsTwoRoots(t *testing.T) {
	r1, r2 := chi.NewRouter(), chi.NewRouter()
	d1, d2 := routeGen(), routeGen()
	specout.Bind(d1, specoutchi.New(r1)).Get("/a", routeOK)
	specout.Bind(d2, specoutchi.New(r2)).Get("/b", routeOK)
	r2.Get("/stray", routeOKBody)
	require.NoError(t, specout.Check(d1, specoutchi.New(r1)), "root one reported a stray")
	assert.Error(t, specout.Check(d2, specoutchi.New(r2)), "root two stray not reported")
}

type routeFeatureQuery struct {
	ID     string            `path:"id"`
	Filter map[string]string `jsonschema:"style=deepObject,explode=true" query:"filter"`
}

type routeFeatureRes struct {
	OK bool `json:"ok"`
}

func TestFeaturesThroughChi(t *testing.T) {
	d, r := routeGen(), chi.NewRouter()
	rec := recorder.New(nil)
	r.Use(specoutchi.Record(rec))
	routes := specout.Bind(d, specoutchi.New(r))
	routes.Get("/things/{id}", specout.Handler[routeFeatureQuery, routeFeatureRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("fail") != "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
		Responses: []specout.Response{{Key: "4XX", Type: routeFeatureRes{}}},
	})
	routes.Post("/things", specout.Handler[routeFeatureRes, routeFeatureRes]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	})
	params := routeGet(t, d, "/things/{id}")["parameters"].([]any)
	require.Len(t, params, 2)
	for _, target := range []string{"/things/1", "/things/1?fail=1"} {
		routeServe(t, r, http.MethodGet, target)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/things", strings.NewReader("{}")))
	require.NoError(t, recorder.Verify(d, rec))
	require.NoError(t, specout.Check(d, specoutchi.New(r)))
}
