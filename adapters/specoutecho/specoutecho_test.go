package specoutecho_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutecho"
	"github.com/happytoolin/specout/recorder"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGen() *specout.Generator {
	return specout.New(specout.Config{Title: "t", Version: "1"})
}

// docPaths builds d and returns its paths object.
func docPaths(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, d.WriteJSON(&buf))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	return doc["paths"].(map[string]any)
}

// fakeT collects the failures a verification helper reports.
type fakeT struct{ errs []string }

func (*fakeT) Helper() {}

func (f *fakeT) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

// get sends one GET request through h.
func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
	return w
}

// panicMessage runs f and returns what it panicked with.
func panicMessage(t *testing.T, f func()) string {
	t.Helper()
	var got any
	func() {
		defer func() { got = recover() }()
		f()
	}()
	require.NotNil(t, got, "no panic")
	return fmt.Sprint(got)
}

func strays(t *testing.T, err error) []string {
	t.Helper()
	var stray *specout.StrayError
	require.ErrorAs(t, err, &stray)
	return stray.Routes
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
	d, e := newGen(), echo.New()
	g := e.Group("/api").Group("/v1")
	specoutecho.New(d, g).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	pathItem, ok := docPaths(t, d)["/api/v1/users/{id}"].(map[string]any)
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
			d, e := newGen(), echo.New()
			specoutecho.New(d, e).Get(tc.route, specout.Handler[struct{}, specout.NoContent]{
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
	d, e := newGen(), echo.New()
	var order []string
	mw := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			order = append(order, "middleware")
			return next(c)
		}
	}
	specoutecho.New(d, e).Get("/x", specout.Handler[struct{}, specout.NoContent]{
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := newGen(), echo.New()
			var routes specoutecho.Routes = e
			if tc.group != "" {
				routes = e.Group(tc.group)
			}
			specoutecho.New(d, routes).Get(tc.route, okRoute)
			paths := docPaths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specoutecho.Adopt(d, e), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		register   func(r *specoutecho.Router)
		newEcho    func() *echo.Echo
	}{
		{"literal braces", "literal braces", func(r *specoutecho.Router) { r.Get("/a/{b}", okRoute) }, echo.New},
		{"duplicate on a rejecting router", "duplicate route", func(r *specoutecho.Router) { r.Get("/ok", okRoute) }, strictEcho},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := newGen(), tc.newEcho()
			r := specoutecho.New(d, e)
			r.Get("/ok", okRoute)
			msg := panicMessage(t, func() { tc.register(r) })
			assert.Contains(t, msg, "specoutecho: GET")
			assert.Contains(t, msg, tc.want)
			assert.Equal(t, []string{"/ok"}, slices.Sorted(maps.Keys(docPaths(t, d))))
			assert.NoError(t, specoutecho.Adopt(d, e))
		})
	}
}

// strictEcho is Echo with route overwriting off, so Echo itself rejects a
// duplicate method and path.
func strictEcho() *echo.Echo {
	return echo.NewWithConfig(echo.Config{Router: echo.NewRouter(echo.RouterConfig{})})
}

// echo.New allows overwriting a route, so a duplicate reaches the router and
// replaces the first handler. Registration should still fail loud.
func TestDuplicateRegistrationPanics(t *testing.T) {
	d, e := newGen(), echo.New()
	r := specoutecho.New(d, e)
	r.Get("/ok", okRoute)
	assert.Panics(t, func() { r.Get("/ok", okRoute) }, "a duplicate GET /ok registers silently")
	assert.NoError(t, d.WriteJSON(&bytes.Buffer{}), "the duplicate left a record")
}

func TestAdoptReportsStrays(t *testing.T) {
	d, e := newGen(), echo.New()
	specoutecho.New(d, e).Get("/documented", okRoute)
	specoutecho.New(d, e.Group("/admin", echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return next
	}))).Get("/users", okRoute)
	e.GET("/plain", nativeRoute)
	e.GET("/openapi.json", nativeRoute)
	e.Any("/any", nativeRoute)
	got := strays(t, specoutecho.Adopt(d, e, specout.Skip("/openapi.json")))
	assert.ElementsMatch(t, []string{"GET /plain", "? /any (no method constraint)"}, got)
}

func TestRequireDocumented(t *testing.T) {
	d, e := newGen(), echo.New()
	specoutecho.New(d, e).Get("/documented", okRoute)
	var pass fakeT
	specoutecho.RequireDocumented(&pass, d, e)
	assert.Empty(t, pass.errs)

	e.GET("/plain", nativeRoute)
	var fail fakeT
	specoutecho.RequireDocumented(&fail, d, e)
	require.Len(t, fail.errs, 1)
	assert.Contains(t, fail.errs[0], "GET /plain")
}

// recorded registers GET /items/:id, declaring 200 and 404, behind Record.
func recorded(t *testing.T) (*specout.Generator, *echo.Echo, *recorder.Recorder) {
	t.Helper()
	d, e := newGen(), echo.New()
	rec := recorder.New(nil, specout.Skip("/openapi.json"))
	e.Use(specoutecho.Record(rec))
	specoutecho.New(d, e).Get("/items/:id", specout.Handler[idReq, item]{
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
	var ft fakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.errs)
}

func TestRecordReportsMissingStatus(t *testing.T) {
	d, e, rec := recorded(t)
	get(t, e, "/items/1")
	var ft fakeT
	recorder.Verify(&ft, d, rec)
	assert.Equal(t, []string{"declared GET /items/{id} 404 never produced by any test"}, ft.errs)
}

func TestRecordCountsMiddlewareResponse(t *testing.T) {
	for name, deny := range map[string]func(c *echo.Context) error{
		"written":  func(c *echo.Context) error { return c.NoContent(http.StatusUnauthorized) },
		"returned": func(*echo.Context) error { return echo.ErrUnauthorized },
	} {
		t.Run(name, func(t *testing.T) {
			d, e := newGen(), echo.New()
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
			specoutecho.New(d, e.Group("/api", auth)).Get("/me", specout.Handler[struct{}, specout.NoContent]{
				HandlerFunc: noContent,
				Responses:   []specout.Response{{Status: http.StatusNoContent}, {Status: http.StatusUnauthorized}},
			})
			assert.Equal(t, http.StatusUnauthorized, get(t, e, "/api/me").Code)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", nil)
			req.Header.Set("Authorization", "yes")
			e.ServeHTTP(httptest.NewRecorder(), req)
			var ft fakeT
			recorder.Verify(&ft, d, rec)
			assert.Empty(t, ft.errs)
		})
	}
}

func TestNewPanicsOnNil(t *testing.T) {
	assert.Panics(t, func() { specoutecho.New(nil, echo.New()) })
	assert.Panics(t, func() { specoutecho.New(newGen(), nil) })
}

// Echo shows the group prefix only after the route is added. A registration
// that panics then, for braces in the prefix or for a duplicate that replaced
// a route, must not leave a live handler behind when the caller recovers.
func TestPanickedRegistrationServesNothing(t *testing.T) {
	d, e := newGen(), echo.New()
	b := specoutecho.New(d, e.Group("/{tenant}"))
	assert.Contains(t, panicMessage(t, func() { b.Get("/items", okRoute) }), "braces")

	specoutecho.New(d, e).Get("/dup", okRoute)
	second := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}}
	assert.Contains(t, panicMessage(t, func() { specoutecho.New(d, e).Get("/dup", second) }), "duplicate route")

	for _, target := range []string{"/{tenant}/items", "/dup"} {
		assert.Equal(t, http.StatusNotFound, get(t, e, target).Code, target)
	}
}
