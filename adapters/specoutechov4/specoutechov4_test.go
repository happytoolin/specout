package specoutechov4_test

import (
	"bytes"
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

// send sends one request through h.
func send(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return w
}

// get sends one GET request through h.
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
	specoutechov4.New(d, g).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	pathItem, ok := adaptertest.Paths(t, d)["/api/v1/users/{id}"].(map[string]any)
	require.True(t, ok, "no /api/v1/users/{id} in the document")
	params := pathItem["get"].(map[string]any)["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
	assert.Equal(t, "path", params[0].(map[string]any)["in"])
	assert.Equal(t, http.StatusNoContent, get(t, e, "/api/v1/users/7").Code)
}

// Echo v4's WrapHandler leaves r.PathValue empty, so the adapter copies
// every parameter Echo sets. The documented names must be the names Echo
// sets, for every route syntax Echo reads.
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
			specoutechov4.New(d, e).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
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

// Echo v4 routes on the encoded path when the request has one, so its values
// stay encoded. The handler sees them decoded, and only once, as on
// http.ServeMux.
func TestHandlerSeesDecodedPathValues(t *testing.T) {
	for target, want := range map[string]string{
		"/users/a%40b%20c": "a@b c",
		"/users/a%2541":    "a%41",
		"/users/plain":     "plain",
	} {
		d, e := adaptertest.Generator(), echo.New()
		specoutechov4.New(d, e).Get("/users/:id", specout.Handler[struct{}, specout.NoContent]{
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
			specoutechov4.New(d, e).Get(route, specout.Handler[struct{}, specout.NoContent]{
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

func TestRouteMiddlewareRunsFirst(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	var order []string
	mw := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			order = append(order, "middleware")
			return next(c)
		}
	}
	specoutechov4.New(d, e).Get("/x", specout.Handler[struct{}, specout.NoContent]{
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
			specoutechov4.New(d, routes).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specoutechov4.Adopt(d, e), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		register   func(r *specoutechov4.Router)
	}{
		{"literal braces", "literal braces", func(r *specoutechov4.Router) { r.Get("/a/{b}", okRoute) }},
		{"colon in a name", "cannot be an OpenAPI parameter", func(r *specoutechov4.Router) { r.Get("/:a-:b", okRoute) }},
		{"duplicate", "duplicate route", func(r *specoutechov4.Router) { r.Get("/ok", okRoute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			r := specoutechov4.New(d, e)
			r.Get("/ok", okRoute)
			msg := adaptertest.PanicMessage(t, func() { tc.register(r) })
			assert.Contains(t, msg, "GET")
			assert.Contains(t, msg, tc.want)
			assert.Equal(t, []string{"/ok"}, slices.Sorted(maps.Keys(adaptertest.Paths(t, d))))
			assert.NoError(t, specoutechov4.Adopt(d, e))
		})
	}
}

// Echo v4 always lets a later route replace an earlier one on its path, so
// the duplicate reaches the router. Registration should still fail loud.
func TestDuplicateRegistrationPanics(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	r := specoutechov4.New(d, e)
	r.Get("/ok", okRoute)
	assert.Panics(t, func() { r.Get("/ok", okRoute) }, "a duplicate GET /ok registers silently")
	assert.NoError(t, d.WriteJSON(&bytes.Buffer{}), "the duplicate left a record")
}

func TestAdoptReportsStrays(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	specoutechov4.New(d, e).Get("/documented", okRoute)
	specoutechov4.New(d, e.Group("/admin", func(next echo.HandlerFunc) echo.HandlerFunc {
		return next
	})).Get("/users", okRoute)
	e.GET("/plain", nativeRoute)
	e.POST("/plain", nativeRoute)
	e.GET("/openapi.json", nativeRoute)
	e.Any("/any", nativeRoute)
	got := adaptertest.Strays(t, specoutechov4.Adopt(d, e, specout.Skip("/openapi.json")))
	assert.Equal(t, []string{"? /any (no method constraint)", "GET /plain", "POST /plain"}, got)
}

func TestAdoptCoversHostRouters(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	api := e.Host("api.example.com")
	specoutechov4.New(d, api).Get("/documented", okRoute)
	api.GET("/plain", nativeRoute)
	assert.Equal(t, []string{"GET /plain"}, adaptertest.Strays(t, specoutechov4.Adopt(d, e)))
}

func TestRequireDocumented(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	specoutechov4.New(d, e).Get("/documented", okRoute)
	var pass adaptertest.FakeT
	specoutechov4.RequireDocumented(&pass, d, e)
	assert.Empty(t, pass.Errs)

	e.GET("/plain", nativeRoute)
	var fail adaptertest.FakeT
	specoutechov4.RequireDocumented(&fail, d, e)
	require.Len(t, fail.Errs, 1)
	assert.Contains(t, fail.Errs[0], "GET /plain")
}

// recorded registers GET /items/:id, declaring 200 and 404, behind Record.
func recorded(t *testing.T) (*specout.Generator, *echo.Echo, *recorder.Recorder) {
	t.Helper()
	d, e := adaptertest.Generator(), echo.New()
	rec := recorder.New(nil, specout.Skip("/openapi.json"))
	e.Use(specoutechov4.Record(rec))
	specoutechov4.New(d, e).Get("/items/:id", specout.Handler[idReq, item]{
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

// Echo v4 sets the context path of a GET route for a HEAD or POST request it
// answers 405, and for the OPTIONS request it answers itself. No route ran.
func TestRecordIgnoresOtherMethods(t *testing.T) {
	d, e, rec := recorded(t)
	get(t, e, "/items/1")
	get(t, e, "/items/missing")
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodOptions} {
		send(t, e, method, "/items/1")
	}
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
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
			specoutechov4.New(d, e.Group("/api", auth)).Get("/me", specout.Handler[struct{}, specout.NoContent]{
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
		})
	}
}

func TestNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specoutechov4.New(nil, echo.New()) })
	assert.Panics(t, func() { specoutechov4.New(adaptertest.Generator(), nil) })
}

// Echo shows the group prefix only after the route is added. A registration
// that panics then, for braces in the prefix or for a duplicate that replaced
// a route, must not leave a live handler behind when the caller recovers.
func TestPanickedRegistrationServesNothing(t *testing.T) {
	d, e := adaptertest.Generator(), echo.New()
	b := specoutechov4.New(d, e.Group("/{tenant}"))
	assert.Contains(t, adaptertest.PanicMessage(t, func() { b.Get("/items", okRoute) }), "braces")

	specoutechov4.New(d, e).Get("/dup", okRoute)
	second := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}}
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specoutechov4.New(d, e).Get("/dup", second) }), "duplicate route")

	for _, target := range []string{"/{tenant}/items", "/dup"} {
		assert.Equal(t, http.StatusNotFound, get(t, e, target).Code, target)
	}
}

// A handler that hijacks the connection writes the response itself. Record
// used to report a 200 for each case of adaptertest.HijackCases.
func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		t.Run(tc.Name, func(t *testing.T) {
			d, e := adaptertest.Generator(), echo.New()
			rec := recorder.New(nil)
			e.Use(specoutechov4.Record(rec))
			specoutechov4.New(d, e).Get("/ws", tc.Route())
			assert.Equal(t, tc.Status, adaptertest.RawStatus(t, e, "/ws", tc.Upgrade))
			var ft adaptertest.FakeT
			recorder.Verify(&ft, d, rec)
			assert.Equal(t, tc.Errs, ft.Errs)
		})
	}
}
