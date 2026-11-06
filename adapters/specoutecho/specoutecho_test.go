package specoutecho_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutecho"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/recorder"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func nativeRoute(c *echo.Context) error {
	c.Response().WriteHeader(http.StatusNoContent)
	return nil
}

func TestGroupRouteDocumentedAtFullPath(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	g := e.Group("/api").Group("/v1")
	specout.Bind(d, specoutecho.New(g)).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
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
		{`/items/:id\:cancel`, "/items/7:cancel", "id", "7"},
		{"/files/*", "/files/a/b.txt", "*", "a/b.txt"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			specout.Bind(d, specoutecho.New(e)).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
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

func TestUnescapePathParamValuesDecodesPathValues(t *testing.T) {
	for _, tc := range []struct {
		unescape bool
		want     string
	}{
		{false, "a%40b%20c"},
		{true, "a@b c"},
	} {
		d := adaptertest.Generator()
		e := echo.NewWithConfig(echo.Config{
			Router: echo.NewRouter(echo.RouterConfig{UnescapePathParamValues: tc.unescape}),
		})
		specout.Bind(d, specoutecho.New(e)).Get("/users/:id", specout.Handler[struct{}, specout.NoContent]{
			HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Value", r.PathValue("id"))
				w.WriteHeader(http.StatusNoContent)
			},
		})
		w := get(t, e, "/users/a%40b%20c")
		require.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, tc.want, w.Header().Get("X-Value"), "UnescapePathParamValues=%v", tc.unescape)
	}
}

func TestGroupMiddlewareRunsFirst(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	var order []string
	mw := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			order = append(order, "middleware")
			return next(c)
		}
	}
	specout.Bind(d, specoutecho.New(e.Group("/g", mw))).Get("/x", specout.Handler[struct{}, specout.NoContent]{
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			var routes specoutecho.Routes = e
			if tc.group != "" {
				routes = e.Group(tc.group)
			}
			specout.Bind(d, specoutecho.New(routes)).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specout.Check(d, specoutecho.New(e)), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		register   func(r *specout.Binder)
		newEcho    func() *echo.Echo
	}{
		{"literal braces", "literal braces", func(r *specout.Binder) { r.Get("/a/{b}", okRoute) }, echo.New},
		{"duplicate on a rejecting router", "duplicate route", func(r *specout.Binder) { r.Get("/ok", okRoute) }, strictEcho},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), tc.newEcho()
			r := specout.Bind(d, specoutecho.New(e))
			r.Get("/ok", okRoute)
			msg := adaptertest.PanicMessage(t, func() { tc.register(r) })
			assert.Contains(t, msg, "specoutecho: GET")
			assert.Contains(t, msg, tc.want)
			assert.Equal(t, []string{"/ok"}, slices.Sorted(maps.Keys(adaptertest.Paths(t, d))))
			assert.NoError(t, specout.Check(d, specoutecho.New(e)))
		})
	}
}

func strictEcho() *echo.Echo {
	return echo.NewWithConfig(echo.Config{Router: echo.NewRouter(echo.RouterConfig{})})
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	r := specout.Bind(d, specoutecho.New(e))
	r.Get("/ok", okRoute)
	assert.Panics(t, func() { r.Get("/ok", okRoute) }, "a duplicate GET /ok registers silently")
	_, err := d.Build()
	assert.NoError(t, err, "the duplicate left a record")
}

func TestCheckReportsStrays(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	specout.Bind(d, specoutecho.New(e)).Get("/documented", okRoute)
	specout.Bind(d, specoutecho.New(e.Group("/admin", echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return next
	})))).Get("/users", okRoute)
	e.GET("/plain", nativeRoute)
	e.GET("/openapi.json", nativeRoute)
	e.Any("/any", nativeRoute)
	got := adaptertest.Strays(t, specout.Check(d, specoutecho.New(e), "/openapi.json"))
	assert.ElementsMatch(t, []string{"GET /plain", "? /any (no method constraint)"}, got)
}

func recorded(t *testing.T) (*specout.Generator, *echo.Echo, *recorder.Recorder) {
	t.Helper()
	d, e := adaptertest.Generator(), echo.New()
	rec := recorder.New(nil, "/openapi.json")
	e.Use(specoutecho.Record(rec))
	specout.Bind(d, specoutecho.New(e)).Get("/items/:id", specout.Handler[idReq, item]{
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
	for name, deny := range map[string]func(c *echo.Context) error{
		"written":  func(c *echo.Context) error { return c.NoContent(http.StatusUnauthorized) },
		"returned": func(*echo.Context) error { return echo.ErrUnauthorized },
	} {
		t.Run(name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			rec := recorder.New(nil)
			e.Use(specoutecho.Record(rec))
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if c.Request().Header.Get("Authorization") == "" {
						return deny(c)
					}
					return next(c)
				}
			}
			specout.Bind(d, specoutecho.New(e.Group("/api", auth))).Get("/me", specout.Handler[struct{}, specout.NoContent]{
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
	assert.Panics(t, func() { specout.Bind(nil, specoutecho.New(echo.New())) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutecho.New(nil)) })
}

func TestBadRegistrationPanics(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	b := specout.Bind(d, specoutecho.New(e.Group("/{tenant}")))
	assert.Contains(t, adaptertest.PanicMessage(t, func() { b.Get("/items", okRoute) }), "braces")

	specout.Bind(d, specoutecho.New(e)).Get("/dup", okRoute)
	second := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}}
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specout.Bind(d, specoutecho.New(e)).Get("/dup", second) }), "duplicate route")
}

func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			rec := recorder.New(nil)
			e.Use(specoutecho.Record(rec))
			specout.Bind(d, specoutecho.New(e)).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, e, "/ws", tc.Upgrade))
			assert.Equal(t, tc.Errs, adaptertest.Errs(recorder.Verify(d, rec)))
		})
	}
}
