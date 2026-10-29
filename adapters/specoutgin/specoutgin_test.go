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

// get sends one GET request through h.
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
	specoutgin.New(d, g).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
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
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			specoutgin.New(d, e).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
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

func TestRouteMiddlewareRunsFirst(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	var order []string
	mw := func(c *gin.Context) {
		order = append(order, "middleware")
		c.Next()
	}
	specoutgin.New(d, e).Get("/x", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
			w.WriteHeader(http.StatusNoContent)
		},
	}, mw)
	assert.Equal(t, http.StatusNoContent, get(t, e, "/x").Code)
	assert.Equal(t, []string{"middleware", "handler"}, order)
}

func TestPathTranslation(t *testing.T) {
	for _, tc := range []struct {
		name, group, route string
		want               string // "" for a catch-all, which the document omits
	}{
		{"catch-all", "", "/files/*filepath", ""},
		{"escaped colon", "", `/a\:b`, "/a:b"},
		{"cleaned join", "/v1/", "/items", "/v1/items"},
		{"trailing slash kept", "/v1", "/items/", "/v1/items/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			var routes specoutgin.Routes = e
			if tc.group != "" {
				routes = e.Group(tc.group)
			}
			specoutgin.New(d, routes).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specoutgin.Adopt(d, e), "a specout route counts as documented")
		})
	}
}

// Gin rewrites \: to : in its route tree on the first request, so the route
// table and c.FullPath() then spell the literal colon like a parameter.
func escapedColonServed(t *testing.T) (*specout.Generator, *gin.Engine, *recorder.Recorder) {
	t.Helper()
	d, e := adaptertest.Generator(), gin.New()
	rec := recorder.New(nil)
	e.Use(specoutgin.Record(rec))
	specoutgin.New(d, e).Get(`/a\:b`, okRoute)
	require.Equal(t, http.StatusNoContent, get(t, e, "/a:b").Code)
	return d, e, rec
}

func TestEscapedColonAdoptAfterFirstRequest(t *testing.T) {
	d, e, _ := escapedColonServed(t)
	require.NoError(t, specoutgin.Adopt(d, e))
}

func TestEscapedColonRecordAfterFirstRequest(t *testing.T) {
	d, _, rec := escapedColonServed(t)
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
}

// Middleware that answers before the handler leaves Record only Gin's full
// path, which spells a literal colon like a parameter after the first request.
func TestEscapedColonRecordWhenMiddlewareAnswers(t *testing.T) {
	for _, tc := range []struct{ route, target string }{
		{`/v1\:batch`, "/v1:batch"},
		{`/v1\:batch/:id`, "/v1:batch/7"},
		{`/items/:id/x\:y`, "/items/7/x:y"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			rec := recorder.New(nil)
			e.Use(specoutgin.Record(rec), func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
			specoutgin.New(d, e).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: noContent,
				Responses:   []specout.Response{{Status: http.StatusUnauthorized}},
			})
			require.Equal(t, http.StatusUnauthorized, get(t, e, tc.target).Code)
			var ft adaptertest.FakeT
			recorder.Verify(&ft, d, rec)
			assert.Empty(t, ft.Errs, "the 401 counts for the documented route")
		})
	}
}

// After the first request, Gin's table spells a literal colon and a parameter
// the same way, so Adopt must find the reading that is documented.
func TestEscapedColonBesideParameterAfterFirstRequest(t *testing.T) {
	for _, tc := range []struct{ route, target string }{
		{`/items/:id/x\:y`, "/items/7/x:y"},
		{`/v1/a\:b/:id`, "/v1/a:b/7"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			specoutgin.New(d, e).Get(tc.route, specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
			require.Equal(t, http.StatusNoContent, get(t, e, tc.target).Code)
			assert.NoError(t, specoutgin.Adopt(d, e))
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		register   func(r *specoutgin.Router)
	}{
		{"literal braces", "specoutgin: GET /a/{b}: literal braces", func(r *specoutgin.Router) { r.Get("/a/{b}", okRoute) }},
		{"bad escape", `specoutgin: GET /a\b: gin escapes only a colon`, func(r *specoutgin.Router) { r.Get(`/a\b`, okRoute) }},
		{"duplicate", "specout: GET /ok: handlers are already registered for path '/ok'", func(r *specoutgin.Router) { r.Get("/ok", okRoute) }},
		{"gin rejects", "specout: GET /a*b: ", func(r *specoutgin.Router) { r.Get("/a*b", okRoute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			r := specoutgin.New(d, e)
			r.Get("/ok", okRoute)
			assert.Contains(t, adaptertest.PanicMessage(t, func() { tc.register(r) }), tc.want)
			assert.Equal(t, []string{"/ok"}, slices.Sorted(maps.Keys(adaptertest.Paths(t, d))))
			assert.NoError(t, specoutgin.Adopt(d, e))
		})
	}
}

func TestAdoptReportsStrays(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	specoutgin.New(d, e).Get("/documented", okRoute)
	e.GET("/plain", nativeRoute)
	e.GET("/openapi.json", nativeRoute)
	e.Any("/any", nativeRoute)
	got := adaptertest.Strays(t, specoutgin.Adopt(d, e, specout.Skip("/openapi.json")))
	assert.Contains(t, got, "GET /plain")
	assert.Contains(t, got, "CONNECT /any")
	assert.Contains(t, got, "GET /any")
	assert.NotContains(t, got, "GET /documented")
	assert.NotContains(t, got, "GET /openapi.json")
}

func TestRequireDocumented(t *testing.T) {
	d, e := adaptertest.Generator(), gin.New()
	specoutgin.New(d, e).Get("/documented", okRoute)
	var pass adaptertest.FakeT
	specoutgin.RequireDocumented(&pass, d, e)
	assert.Empty(t, pass.Errs)

	e.GET("/plain", nativeRoute)
	var fail adaptertest.FakeT
	specoutgin.RequireDocumented(&fail, d, e)
	require.Len(t, fail.Errs, 1)
	assert.Contains(t, fail.Errs[0], "GET /plain")
}

// recorded registers GET /items/:id, declaring 200 and 404, behind Record.
func recorded(t *testing.T) (*specout.Generator, *gin.Engine, *recorder.Recorder) {
	t.Helper()
	d, e := adaptertest.Generator(), gin.New()
	rec := recorder.New(nil, specout.Skip("/openapi.json"))
	e.Use(specoutgin.Record(rec))
	specoutgin.New(d, e).Get("/items/:id", specout.Handler[idReq, item]{
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
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
}

func TestRecordReportsMissingStatus(t *testing.T) {
	d, e, rec := recorded(t)
	get(t, e, "/items/1")
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Equal(t, []string{"declared GET /items/{id} 404 never produced by any test"}, ft.Errs)
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
	specoutgin.New(d, e.Group("/api", auth)).Get("/me", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: noContent,
		Responses:   []specout.Response{{Status: http.StatusNoContent}, {Status: http.StatusUnauthorized}},
	})
	assert.Equal(t, http.StatusUnauthorized, get(t, e, "/api/me").Code)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "yes")
	e.ServeHTTP(httptest.NewRecorder(), req)
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
}

func TestNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specoutgin.New(nil, gin.New()) })
	assert.Panics(t, func() { specoutgin.New(adaptertest.Generator(), nil) })
}

// A handler that hijacks the connection writes the response itself. Record
// used to report a 200 for each case of adaptertest.HijackCases.
func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, e := adaptertest.Generator(), gin.New()
			rec := recorder.New(nil)
			e.Use(specoutgin.Record(rec))
			specoutgin.New(d, e).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, e, "/ws", tc.Upgrade))
			var ft adaptertest.FakeT
			recorder.Verify(&ft, d, rec)
			assert.Equal(t, tc.Errs, ft.Errs)
		})
	}
}
