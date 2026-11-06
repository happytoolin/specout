package specoutgin_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutgin"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
	return w
}

type idReq struct {
	ID string `path:"id"`
}

type item struct {
	Name string `json:"name"`
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

var okRoute = specout.Handler[struct{}, specout.NoContent]{HandlerFunc: noContent}

func nativeRoute(c *gin.Context) { c.Status(http.StatusNoContent) }

func TestGroupRouteDocumentedAtFullPath(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	g := e.Group("/api").Group("/v1")
	specout.Bind(d, specoutgin.New(g)).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	pathItem, ok := adaptertest.Paths(t, d)["/api/v1/users/{id}"].(map[string]any)
	require.True(t, ok, "no /api/v1/users/{id} in the document")
	params := pathItem["get"].(map[string]any)["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
	assert.Equal(t, "path", params[0].(map[string]any)["in"])
	assert.Equal(t, http.StatusNoContent, get(t, e, "/api/v1/users/7").Code)
}

func TestHandlerSeesPathValues(t *testing.T) {
	for _, tc := range []struct{ route, target, name, want string }{
		{"/users/:id", "/users/42", "id", "42"},
		{"/files/*filepath", "/files/a/b.txt", "filepath", "/a/b.txt"},
		{"/files/*filepath", "/files/", "filepath", "/"},
		{"/:dir/*filepath", "/d/a", "dir", "d"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			specout.Bind(d, specoutgin.New(e)).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Value", r.PathValue(tc.name))
					w.WriteHeader(http.StatusNoContent)
				},
			})
			w := get(t, e, tc.target)
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("X-Value"))
		})
	}
}

func TestPathValuesFollowGinUnescaping(t *testing.T) {
	for _, tc := range []struct {
		raw, unescape bool
		want          string
	}{
		{false, true, "a@b"},
		{true, true, "a@b"},
		{true, false, "a%40b"},
	} {
		d, e := adaptertest.Generator(), gin.New()
		e.UseRawPath, e.UnescapePathValues = tc.raw, tc.unescape
		specout.Bind(d, specoutgin.New(e)).Get("/users/:id", specout.Handler[struct{}, specout.NoContent]{
			HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Value", r.PathValue("id"))
				w.WriteHeader(http.StatusNoContent)
			},
		})
		assert.Equal(t, tc.want, get(t, e, "/users/a%40b").Header().Get("X-Value"), "%+v", tc)
	}
}

func TestGroupMiddlewareRunsFirst(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	var order []string
	mw := func(c *gin.Context) {
		order = append(order, "middleware")
		c.Next()
	}
	specout.Bind(d, specoutgin.New(e.Group("/g", mw))).Get("/x", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
			w.WriteHeader(http.StatusNoContent)
		},
	})
	assert.Equal(t, http.StatusNoContent, get(t, e, "/g/x").Code)
	assert.Equal(t, []string{"middleware", "handler"}, order)
}

func TestPathTranslation(t *testing.T) {
	for _, tc := range []struct {
		name, group, route string
		want               string
	}{
		{"catch-all", "", "/files/*filepath", ""},
		{"cleaned join", "/v1/", "/items", "/v1/items"},
		{"trailing slash kept", "/v1", "/items/", "/v1/items/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			var routes specoutgin.Routes = e
			if tc.group != "" {
				routes = e.Group(tc.group)
			}
			specout.Bind(d, specoutgin.New(routes)).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specout.Check(d, specoutgin.New(e)), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		register   func(r *specout.Binder)
	}{
		{"literal braces", "specoutgin: GET /a/{b}: literal braces", func(r *specout.Binder) { r.Get("/a/{b}", okRoute) }},
		{"escape", `specoutgin: GET /a\:b: escaped characters`, func(r *specout.Binder) { r.Get(`/a\:b`, okRoute) }},
		{"duplicate", "specout: GET /ok: handlers are already registered for path '/ok'", func(r *specout.Binder) { r.Get("/ok", okRoute) }},
		{"gin rejects", "specout: GET /a*b: ", func(r *specout.Binder) { r.Get("/a*b", okRoute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			r := specout.Bind(d, specoutgin.New(e))
			r.Get("/ok", okRoute)
			assert.Contains(t, adaptertest.PanicMessage(t, func() { tc.register(r) }), tc.want)
			assert.Equal(t, []string{"/ok"}, slices.Sorted(maps.Keys(adaptertest.Paths(t, d))))
			assert.NoError(t, specout.Check(d, specoutgin.New(e)))
		})
	}
}

func TestDuplicateRoutePanics(t *testing.T) {
	d, first, second := adaptertest.Generator(), gin.New(), gin.New()
	specout.Bind(d, specoutgin.New(first)).Get("/dup", okRoute)
	teapot := specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) },
	}
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specout.Bind(d, specoutgin.New(second)).Get("/dup", teapot) }),
		"duplicate route")
}

func TestCheckReportsStrays(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	specout.Bind(d, specoutgin.New(e)).Get("/documented", okRoute)
	e.GET("/plain", nativeRoute)
	e.GET("/openapi.json", nativeRoute)
	e.Any("/any", nativeRoute)
	got := adaptertest.Strays(t, specout.Check(d, specoutgin.New(e), "/openapi.json"))
	assert.Contains(t, got, "GET /plain")
	assert.Contains(t, got, "CONNECT /any")
	assert.Contains(t, got, "GET /any")
	assert.NotContains(t, got, "GET /documented")
	assert.NotContains(t, got, "GET /openapi.json")
}

func recorded(t *testing.T) (*specout.Generator, *gin.Engine, *recorder.Recorder) {
	t.Helper()
	d, e := adaptertest.Generator(), gin.New()
	rec := recorder.New(nil, "/openapi.json")
	e.Use(specoutgin.Record(rec))
	specout.Bind(d, specoutgin.New(e)).Get("/items/:id", specout.Handler[idReq, item]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("id") == "missing" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
		Responses: []specout.Response{{Status: http.StatusNotFound}},
	})
	e.GET("/openapi.json", nativeRoute)
	return d, e, rec
}

func TestRecordAllStatusesObserved(t *testing.T) {
	d, e, rec := recorded(t)
	get(t, e, "/items/1")
	get(t, e, "/items/missing")
	get(t, e, "/openapi.json")
	get(t, e, "/unknown")
	assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecordReportsMissingStatus(t *testing.T) {
	d, e, rec := recorded(t)
	get(t, e, "/items/1")
	assert.Equal(t, []string{"declared GET /items/{id} 404 never produced by any test"}, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecordCountsMiddlewareResponse(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	rec := recorder.New(nil)
	e.Use(specoutgin.Record(rec))
	auth := func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
	specout.Bind(d, specoutgin.New(e.Group("/api", auth))).Get("/me", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: noContent,
		Responses:   []specout.Response{{Status: http.StatusNoContent}, {Status: http.StatusUnauthorized}},
	})
	assert.Equal(t, http.StatusUnauthorized, get(t, e, "/api/me").Code)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "yes")
	e.ServeHTTP(httptest.NewRecorder(), req)
	assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specout.Bind(nil, specoutgin.New(gin.New())) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutgin.New(nil)) })
}

func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			rec := recorder.New(nil)
			e.Use(specoutgin.Record(rec))
			specout.Bind(d, specoutgin.New(e)).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, e, "/ws", tc.Upgrade))
			assert.Equal(t, tc.Errs, adaptertest.Errs(recorder.Verify(d, rec)))
		})
	}
}
