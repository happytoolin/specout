package specoutechov4_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutechov4"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/recorder"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func send(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return w
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, h, http.MethodGet, target)
}

type idReq struct {
	ID string `path:"id"`
}

type item struct {
	Name string `json:"name"`
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

var okRoute = specout.Handler[struct{}, specout.NoContent]{HandlerFunc: noContent}

func nativeRoute(c echo.Context) error {
	c.Response().WriteHeader(http.StatusNoContent)
	return nil
}

func TestGroupRouteDocumentedAtFullPath(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	g := e.Group("/api").Group("/v1")
	specout.Bind(d, specoutechov4.New(g)).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	pathItem, ok := adaptertest.Paths(t, d)["/api/v1/users/{id}"].(map[string]any)
	require.True(t, ok, "no /api/v1/users/{id} in the document")
	params := pathItem["get"].(map[string]any)["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
	assert.Equal(t, "path", params[0].(map[string]any)["in"])
	assert.Equal(t, http.StatusNoContent, get(t, e, "/api/v1/users/7").Code)
}

func TestHandlerSeesPathValues(t *testing.T) {
	for _, tc := range []struct {
		route, target, documented string
		values                    map[string]string
	}{
		{"/users/:id", "/users/42", "/users/{id}", map[string]string{"id": "42"}},
		{"/:a/:b", "/1/2", "/{a}/{b}", map[string]string{"a": "1", "b": "2"}},
		{"/v:ver", "/v3", "/v{ver}", map[string]string{"ver": "3"}},
		{`/items/:id\:cancel`, "/items/7:cancel", "/items/{id}:cancel", map[string]string{"id": "7"}},
		{`/:name\:x/z`, "/n:x/z", "/{name}:x/z", map[string]string{"name": "n"}},
		{`/a\:b/:id`, "/a:b/9", "/a:b/{id}", map[string]string{"id": "9"}},
		{"no-slash/:id", "/no-slash/5", "/no-slash/{id}", map[string]string{"id": "5"}},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			specout.Bind(d, specoutechov4.New(e)).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
					for name := range tc.values {
						w.Header().Set("X-"+name, r.PathValue(name))
					}
					w.WriteHeader(http.StatusNoContent)
				},
			})
			assert.Contains(t, adaptertest.Paths(t, d), tc.documented)
			w := get(t, e, tc.target)
			require.Equal(t, http.StatusNoContent, w.Code)
			for name, want := range tc.values {
				assert.Equal(t, want, w.Header().Get("X-"+name), name)
			}
		})
	}
}

func TestHandlerSeesEchoPathValues(t *testing.T) {
	for target, want := range map[string]string{
		"/users/a%40b":  "a%40b",
		"/users/a%2541": "a%41",
		"/users/plain":  "plain",
	} {
		d, e := adaptertest.Generator(), echo.New()
		specout.Bind(d, specoutechov4.New(e)).Get("/users/:id", specout.Handler[struct{}, specout.NoContent]{
			HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Value", r.PathValue("id"))
				w.WriteHeader(http.StatusNoContent)
			},
		})
		w := get(t, e, target)
		require.Equal(t, http.StatusNoContent, w.Code, target)
		assert.Equal(t, want, w.Header().Get("X-Value"), target)
	}
}

func TestCatchAllSeesRest(t *testing.T) {
	for _, route := range []string{"/files/*", "/files/*rest", "/files/*/ignored"} {
		t.Run(route, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			specout.Bind(d, specoutechov4.New(e)).Get(route, specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Rest", r.PathValue("*"))
					w.WriteHeader(http.StatusNoContent)
				},
			})
			assert.Empty(t, adaptertest.Paths(t, d), "a catch-all is not an OpenAPI path")
			w := get(t, e, "/files/a/b.txt")
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Equal(t, "a/b.txt", w.Header().Get("X-Rest"))
		})
	}
}

func TestGroupMiddlewareRunsFirst(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	var order []string
	mw := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			order = append(order, "middleware")
			return next(c)
		}
	}
	specout.Bind(d, specoutechov4.New(e.Group("/g", mw))).Get("/x", specout.Handler[struct{}, specout.NoContent]{
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
		{"inline verb", "", `/items/:id\:cancel`, "/items/{id}:cancel"},
		{"escaped colon", "", `/a\:b`, "/a:b"},
		{"catch-all", "", "/files/*", ""},
		{"join without slash", "/v1", "posts", "/v1posts"},
		{"empty path", "", "", "/"},
		{"empty group path", "/v1", "", "/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			var routes specoutechov4.Routes = e
			if tc.group != "" {
				routes = e.Group(tc.group)
			}
			specout.Bind(d, specoutechov4.New(routes)).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specout.Check(d, specoutechov4.New(e)), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		register   func(r *specout.Binder)
	}{
		{"literal braces", "literal braces", func(r *specout.Binder) { r.Get("/a/{b}", okRoute) }},
		{"colon in a name", "cannot be an OpenAPI parameter", func(r *specout.Binder) { r.Get("/:a-:b", okRoute) }},
		{"duplicate", "duplicate route", func(r *specout.Binder) { r.Get("/ok", okRoute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			r := specout.Bind(d, specoutechov4.New(e))
			r.Get("/ok", okRoute)
			msg := adaptertest.PanicMessage(t, func() { tc.register(r) })
			assert.Contains(t, msg, "GET")
			assert.Contains(t, msg, tc.want)
			assert.Equal(t, []string{"/ok"}, slices.Sorted(maps.Keys(adaptertest.Paths(t, d))))
			assert.NoError(t, specout.Check(d, specoutechov4.New(e)))
		})
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	r := specout.Bind(d, specoutechov4.New(e))
	r.Get("/ok", okRoute)
	assert.Panics(t, func() { r.Get("/ok", okRoute) }, "a duplicate GET /ok registers silently")
	_, err := d.Build()
	assert.NoError(t, err, "the duplicate left a record")
}

func TestCheckReportsStrays(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	specout.Bind(d, specoutechov4.New(e)).Get("/documented", okRoute)
	specout.Bind(d, specoutechov4.New(e.Group("/admin", func(next echo.HandlerFunc) echo.HandlerFunc {
		return next
	}))).Get("/users", okRoute)
	e.GET("/plain", nativeRoute)
	e.POST("/plain", nativeRoute)
	e.GET("/openapi.json", nativeRoute)
	e.Any("/any", nativeRoute)
	got := adaptertest.Strays(t, specout.Check(d, specoutechov4.New(e), "/openapi.json"))
	assert.Equal(t, []string{
		"CONNECT /any", "DELETE /any", "GET /any", "GET /plain", "HEAD /any", "OPTIONS /any", "PATCH /any",
		"POST /any", "POST /plain", "PROPFIND /any", "PUT /any", "REPORT /any", "TRACE /any",
	}, got)
}

func TestCheckCoversHostRouters(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	api := e.Host("api.example.com")
	specout.Bind(d, specoutechov4.New(api)).Get("/documented", okRoute)
	api.GET("/plain", nativeRoute)
	assert.Equal(t, []string{"GET /plain"}, adaptertest.Strays(t, specout.Check(d, specoutechov4.New(e))))
}

func recorded(t *testing.T) (*specout.Generator, *echo.Echo, *recorder.Recorder) {
	t.Helper()
	d, e := adaptertest.Generator(), echo.New()
	rec := recorder.New(nil, "/openapi.json")
	e.Use(specoutechov4.Record(rec))
	specout.Bind(d, specoutechov4.New(e)).Get("/items/:id", specout.Handler[idReq, item]{
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

func TestRecordIgnoresOtherMethods(t *testing.T) {
	d, e, rec := recorded(t)
	get(t, e, "/items/1")
	get(t, e, "/items/missing")
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodOptions} {
		send(t, e, method, "/items/1")
	}
	assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecordCountsMiddlewareResponse(t *testing.T) {
	for name, deny := range map[string]func(c echo.Context) error{
		"written":  func(c echo.Context) error { return c.NoContent(http.StatusUnauthorized) },
		"returned": func(echo.Context) error { return echo.ErrUnauthorized },
	} {
		t.Run(name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			rec := recorder.New(nil)
			e.Use(specoutechov4.Record(rec))
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					if c.Request().Header.Get("Authorization") == "" {
						return deny(c)
					}
					return next(c)
				}
			}
			specout.Bind(d, specoutechov4.New(e.Group("/api", auth))).Get("/me", specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: noContent,
				Responses:   []specout.Response{{Status: http.StatusNoContent}, {Status: http.StatusUnauthorized}},
			})
			assert.Equal(t, http.StatusUnauthorized, get(t, e, "/api/me").Code)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", nil)
			req.Header.Set("Authorization", "yes")
			e.ServeHTTP(httptest.NewRecorder(), req)
			assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
		})
	}
}

func TestNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specout.Bind(nil, specoutechov4.New(echo.New())) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutechov4.New(nil)) })
}

func TestBadRegistrationPanics(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	b := specout.Bind(d, specoutechov4.New(e.Group("/{tenant}")))
	assert.Contains(t, adaptertest.PanicMessage(t, func() { b.Get("/items", okRoute) }), "braces")

	specout.Bind(d, specoutechov4.New(e)).Get("/dup", okRoute)
	second := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}}
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specout.Bind(d, specoutechov4.New(e)).Get("/dup", second) }), "duplicate route")
}

func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			rec := recorder.New(nil)
			e.Use(specoutechov4.Record(rec))
			specout.Bind(d, specoutechov4.New(e)).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, e, "/ws", tc.Upgrade))
			assert.Equal(t, tc.Errs, adaptertest.Errs(recorder.Verify(d, rec)))
		})
	}
}
