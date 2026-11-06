package specoutgorilla_test

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
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

func routeServe(t *testing.T, h http.Handler, target string) int {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
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
	d, r := routeGen(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/items/{id:[0-9]*}", routeOK)
	require.Equal(t, http.StatusNoContent, routeServe(t, r, "/items/123"))
	assert.Contains(t, adaptertest.Paths(t, d), "/items/{id}")
}

func TestHostOnlySubrouterPassesCheck(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(r.Host("example.com").Subrouter())).Get("/items", routeOK)
	require.Equal(t, http.StatusNoContent, routeServe(t, r, "http://example.com/items"))
	require.NoError(t, specout.Check(d, specoutgorilla.New(r)))
	assert.Contains(t, adaptertest.Paths(t, d), "/items")
}

func TestDuplicateRouteFails(t *testing.T) {
	routes := specout.Bind(routeGen(), specoutgorilla.New(mux.NewRouter()))
	routes.Get("/x", routeOK)
	assert.Contains(t, routePanic(func() { routes.Get("/x", routeNoBody{HandlerFunc: routeOKBody}) }), "duplicate route")
}

func TestBinderServesAndDocuments(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	routes := specout.Bind(d, specoutgorilla.New(r))
	routes.Get("/items/{id}", routeOK)
	routes.Post("/items", routeOK)
	assert.ElementsMatch(t, []string{"/items", "/items/{id}"}, routeKeys(t, d))
	require.Equal(t, http.StatusNoContent, routeServe(t, r, "/items/9"))
}

func TestHandlerSeesDecodedPathValues(t *testing.T) {
	r := mux.NewRouter()
	var id, name string
	specout.Bind(routeGen(), specoutgorilla.New(r)).Get("/items/{id:[0-9]+}/{name}", routeNoBody{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			id, name = r.PathValue("id"), r.PathValue("name")
			w.WriteHeader(http.StatusNoContent)
		},
	})
	routeServe(t, r, "/items/42/a%40b")
	assert.Equal(t, "42", id)
	assert.Equal(t, "a@b", name)
}

func TestEncodedPathKeepsValuesEncoded(t *testing.T) {
	r := mux.NewRouter().UseEncodedPath()
	var name string
	specout.Bind(routeGen(), specoutgorilla.New(r)).Get("/items/{name}", routeNoBody{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			name = r.PathValue("name")
			w.WriteHeader(http.StatusNoContent)
		},
	})
	routeServe(t, r, "/items/a%40b")
	assert.Equal(t, "a%40b", name)
}

func TestCheckStrayAndSkip(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/ok", routeOK)
	r.HandleFunc("/stray", routeOKBody).Methods(http.MethodGet)
	require.ErrorContains(t, specout.Check(d, specoutgorilla.New(r)), "/stray")
	require.NoError(t, specout.Check(d, specoutgorilla.New(r), "/stray"), "skip should suppress")
}

func TestCheckReportsAMethodlessRoute(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/x", routeOKBody)
	require.ErrorContains(t, specout.Check(routeGen(), specoutgorilla.New(r)), "no method constraint")
}

func TestSubrouterPrefixComposed(t *testing.T) {
	d, root := routeGen(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(root.PathPrefix("/api").Subrouter())).Get("/items", routeOK)
	require.Contains(t, adaptertest.Paths(t, d), "/api/items", "prefix lost")
	require.Equal(t, http.StatusNoContent, routeServe(t, root, "/api/items"))
	require.NoError(t, specout.Check(d, specoutgorilla.New(root)), "subrouter parent flagged as stray")
}

func TestRegexParamPathNormalized(t *testing.T) {
	d := routeGen()
	specout.Bind(d, specoutgorilla.New(mux.NewRouter())).Get("/items/{id:[0-9]+}", routeOK)
	assert.Equal(t, "getItemsId", routeGet(t, d, "/items/{id}")["operationId"])
}

func TestBraceQuantifierParamNormalized(t *testing.T) {
	d := routeGen()
	specout.Bind(d, specoutgorilla.New(mux.NewRouter())).Get("/x/{id:[0-9]{4}}", routeOK)
	assert.Equal(t, "getXId", routeGet(t, d, "/x/{id}")["operationId"])
}

func TestDocPathCollisionFails(t *testing.T) {
	d := routeGen()
	routes := specout.Bind(d, specoutgorilla.New(mux.NewRouter()))
	routes.Get("/x/{id:[0-9]+}", routeNoBody{HandlerFunc: routeOKBody, OperationID: "alpha"})
	assert.Contains(t, routePanic(func() {
		routes.Get("/x/{id:[a-z]+}", routeNoBody{HandlerFunc: routeOKBody, OperationID: "beta"})
	}), `specout: GET /x/{id}: duplicate route, registered as "/x/{id:[0-9]+}" and "/x/{id:[a-z]+}"`)
	assert.Equal(t, "alpha", routeGet(t, d, "/x/{id}")["operationId"])
}

func TestSpecEndpointIsSkipped(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/ok", routeOK)
	r.Handle("/openapi.json", http.NotFoundHandler())
	require.Error(t, specout.Check(d, specoutgorilla.New(r)))
	require.NoError(t, specout.Check(d, specoutgorilla.New(r), "/openapi.json"))
}

func TestBraceInParameterNameFails(t *testing.T) {
	d := routeGen()
	specout.Bind(d, specoutgorilla.New(mux.NewRouter())).Get("/x/{a{b}}", routeOK)
	routeBuildErr(t, d, "a path parameter name has a brace")
}

type routeValueHandler struct{}

func (routeValueHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func TestCheckReportsStructHandler(t *testing.T) {
	r := mux.NewRouter()
	r.Handle("/x", routeValueHandler{}).Methods(http.MethodGet)
	var err error
	require.NotPanics(t, func() { err = specout.Check(routeGen(), specoutgorilla.New(r)) })
	assert.Contains(t, adaptertest.Strays(t, err), "GET /x")
}

func TestRegistrationFailureDoesNotMutateRouter(t *testing.T) {
	for failure, h := range map[string]routeNoBody{"nil": {}, "frozen": routeOK} {
		t.Run(failure, func(t *testing.T) {
			d, r := routeGen(), mux.NewRouter()
			if failure == "frozen" {
				_, err := d.Build()
				require.NoError(t, err)
			}
			require.Panics(t, func() { specout.Bind(d, specoutgorilla.New(r)).Get("/late", h) })
			assert.Equal(t, http.StatusNotFound, routeServe(t, r, "/late"))
		})
	}
}

func TestBadTemplatePanicsAndNamesTheRoute(t *testing.T) {
	for _, pattern := range []string{"/items/{id", "items", "", "/a//b", "/a/./b", "/a/../b"} {
		t.Run(pattern, func(t *testing.T) {
			d := routeGen()
			msg := routePanic(func() { specout.Bind(d, specoutgorilla.New(mux.NewRouter())).Get(pattern, routeOK) })
			assert.Contains(t, msg, "specout: GET "+pattern)
			assert.Empty(t, adaptertest.Paths(t, d))
		})
	}
	sub := mux.NewRouter().PathPrefix("/a//b").Subrouter()
	msg := routePanic(func() { specout.Bind(routeGen(), specoutgorilla.New(sub)).Get("/c", routeOK) })
	assert.Contains(t, msg, "the path has an empty segment (//); remove the extra /")
}

func TestPathlessRouteIsSkippable(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(r)).Get("/x", routeOK)
	r.Methods(http.MethodOptions).HandlerFunc(routeOKBody)
	assert.Equal(t, []string{"OPTIONS *"}, adaptertest.Strays(t, specout.Check(d, specoutgorilla.New(r))))
	require.NoError(t, specout.Check(d, specoutgorilla.New(r), "*"))
}

func TestMountedRouterIsNotAStray(t *testing.T) {
	d, root, api := routeGen(), mux.NewRouter(), mux.NewRouter()
	specout.Bind(d, specoutgorilla.New(api)).Get("/api/x", routeOK)
	root.PathPrefix("/api").Handler(api)
	require.NoError(t, specout.Check(d, specoutgorilla.New(root)))
}

func TestSlashRegexIsAParameter(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	routes := specout.Bind(d, specoutgorilla.New(r))
	routes.Get("/files/{path:.*}", routeOK)
	routes.Get("/blobs/{key:[a-z0-9/]+}", routeOK)
	routes.Get("/items/{id:[0-9]+}", routeOK)
	routes.Get("/names/{name:[^/]+}", routeOK)
	assert.ElementsMatch(t, []string{"/files/{path}", "/blobs/{key}", "/items/{id}", "/names/{name}"}, routeKeys(t, d))
	require.NoError(t, specout.Check(d, specoutgorilla.New(r)))
}

type routeFeatureQuery struct {
	ID     string            `path:"id"`
	Filter map[string]string `jsonschema:"style=deepObject,explode=true" query:"filter"`
}

type routeFeatureRes struct {
	OK bool `json:"ok"`
}

func TestFeaturesThroughGorilla(t *testing.T) {
	d, r := routeGen(), mux.NewRouter()
	rec := recorder.New(nil)
	r.Use(specoutgorilla.Record(rec))
	routes := specout.Bind(d, specoutgorilla.New(r))
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
		routeServe(t, r, target)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/things", strings.NewReader("{}")))
	require.NoError(t, recorder.Verify(d, rec))
	require.NoError(t, specout.Check(d, specoutgorilla.New(r)))
}
