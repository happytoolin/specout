package specoutfiber_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutfiber"
	"github.com/happytoolin/specout/internal/adaptertest"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// do sends one request through app and returns the status and headers.
func do(t *testing.T, app *fiber.App, method, target string) (int, http.Header) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), method, target, nil))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, resp.Header
}

type idReq struct {
	ID string `path:"id"`
}

type item struct {
	Name string `json:"name"`
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

var okRoute = specout.Handler[struct{}, specout.NoContent]{HandlerFunc: noContent}

func nativeRoute(c fiber.Ctx) error {
	c.Status(http.StatusNoContent)
	return nil
}

func TestGroupRouteDocumentedAtFullPath(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	g := app.Group("/api").Group("/v1")
	specoutfiber.New(d, g).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	pathItem, ok := adaptertest.Paths(t, d)["/api/v1/users/{id}"].(map[string]any)
	require.True(t, ok, "no /api/v1/users/{id} in the document")
	params := pathItem["get"].(map[string]any)["parameters"].([]any)
	require.Len(t, params, 1)
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
	assert.Equal(t, "path", params[0].(map[string]any)["in"])
	status, _ := do(t, app, http.MethodGet, "/api/v1/users/7")
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHandlerSeesPathValues(t *testing.T) {
	for _, tc := range []struct{ route, target, name, want string }{
		{"/users/:id", "/users/42", "id", "42"},
		{"/flights/:from-:to", "/flights/LAX-SFO", "to", "SFO"},
		{"/files/*", "/files/a/b.txt", "*1", "a/b.txt"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			specoutfiber.New(d, app).Get(tc.route, pathValueRoute(tc.name))
			status, header := do(t, app, http.MethodGet, tc.target)
			require.Equal(t, http.StatusNoContent, status)
			assert.Equal(t, tc.want, header.Get("X-Value"))
		})
	}
}

// pathValueRoute answers with r.PathValue(name) in the X-Value header.
func pathValueRoute(name string) specout.Handler[struct{}, specout.NoContent] {
	return specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Value", r.PathValue(name))
			w.WriteHeader(http.StatusNoContent)
		},
	}
}

// Fiber leaves path values encoded unless the app unescapes the path. The
// handler sees them decoded either way, and only once, as on http.ServeMux.
func TestHandlerSeesDecodedPathValues(t *testing.T) {
	for _, tc := range []struct {
		cfg          fiber.Config
		target, want string
	}{
		{fiber.Config{}, "/users/a%40b%20c", "a@b c"},
		{fiber.Config{UnescapePath: true}, "/users/a%2541", "a%41"},
	} {
		d, app := adaptertest.Generator(), fiber.New(tc.cfg)
		specoutfiber.New(d, app).Get("/users/:id", pathValueRoute("id"))
		status, header := do(t, app, http.MethodGet, tc.target)
		require.Equal(t, http.StatusNoContent, status)
		assert.Equal(t, tc.want, header.Get("X-Value"), tc.target)
	}
}

func TestRouteMiddlewareRunsFirst(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	var order []string
	mw := func(c fiber.Ctx) error {
		order = append(order, "middleware")
		return c.Next()
	}
	specoutfiber.New(d, app).Get("/x", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
			w.WriteHeader(http.StatusNoContent)
		},
	}, mw)
	status, _ := do(t, app, http.MethodGet, "/x")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, []string{"middleware", "handler"}, order)
}

func TestPathTranslation(t *testing.T) {
	for _, tc := range []struct {
		route string
		want  string // "" for a catch-all, which the document omits
	}{
		{"/users/:id<int>", "/users/{id}"},
		{"/flights/:from-:to", "/flights/{from}-{to}"},
		{"/files/*", ""},
		{`/a\:b`, "/a:b"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			specoutfiber.New(d, app).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specoutfiber.Adopt(d, app), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct{ route, want string }{
		{"/a/{b}", "specoutfiber: GET /a/{b}: literal braces"},
		{"/users/:id?", "specoutfiber: GET /users/:id?: optional parameter :id?"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			r := specoutfiber.New(d, app)
			r.Get("/ok", okRoute)
			assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get(tc.route, okRoute) }), tc.want)
			assert.Len(t, adaptertest.Paths(t, d), 1)
			assert.NoError(t, specoutfiber.Adopt(d, app))
		})
	}
}

func TestAdoptReportsStrays(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	app.Use(func(c fiber.Ctx) error { return c.Next() })
	specoutfiber.New(d, app).Get("/documented", okRoute)
	app.Get("/plain", nativeRoute)
	app.Get("/openapi.json", nativeRoute)
	do(t, app, http.MethodGet, "/documented") // starting the app adds the HEAD routes
	got := adaptertest.Strays(t, specoutfiber.Adopt(d, app, specout.Skip("/openapi.json")))
	assert.Equal(t, []string{"GET /plain"}, got)
}

func TestRequireDocumented(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiber.New(d, app).Get("/documented", okRoute)
	var pass adaptertest.FakeT
	specoutfiber.RequireDocumented(&pass, d, app)
	assert.Empty(t, pass.Errs)

	app.Get("/plain", nativeRoute)
	var fail adaptertest.FakeT
	specoutfiber.RequireDocumented(&fail, d, app)
	require.Len(t, fail.Errs, 1)
	assert.Contains(t, fail.Errs[0], "GET /plain")
}

// recorded registers GET /items/:id, declaring 200 and 404, behind Record.
func recorded(t *testing.T) (*specout.Generator, *fiber.App, *recorder.Recorder) {
	t.Helper()
	d, app := adaptertest.Generator(), fiber.New()
	rec := recorder.New(nil, specout.Skip("/openapi.json"))
	app.Use(specoutfiber.Record(rec))
	specoutfiber.New(d, app).Get("/items/:id", specout.Handler[idReq, item]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("id") == "missing" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
		Responses: []specout.Response{{Status: http.StatusNotFound}},
	})
	app.Get("/openapi.json", nativeRoute)
	return d, app, rec
}

func TestRecordAllStatusesObserved(t *testing.T) {
	d, app, rec := recorded(t)
	do(t, app, http.MethodGet, "/items/1")
	do(t, app, http.MethodGet, "/items/missing")
	do(t, app, http.MethodGet, "/openapi.json")
	do(t, app, http.MethodGet, "/unknown")
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
}

func TestRecordReportsMissingStatus(t *testing.T) {
	d, app, rec := recorded(t)
	do(t, app, http.MethodGet, "/items/1")
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Equal(t, []string{"declared GET /items/{id} 404 never produced by any test"}, ft.Errs)
}

func TestRecordHeadCountsForGet(t *testing.T) {
	d, app, rec := recorded(t)
	do(t, app, http.MethodHead, "/items/1")
	do(t, app, http.MethodHead, "/items/missing")
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
}

func TestRecorderWithoutRouterPanics(t *testing.T) {
	rec := recorder.New(nil)
	assert.Panics(t, func() {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	})
}

func TestNewPanics(t *testing.T) {
	app := fiber.New()
	assert.Panics(t, func() { specoutfiber.New(nil, app) })
	assert.Panics(t, func() { specoutfiber.New(adaptertest.Generator(), nil) })
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specoutfiber.New(adaptertest.Generator(), app.Domain("x.com")) }),
		"specoutfiber: New needs a non-nil *fiber.App or *fiber.Group")
	for _, r := range []fiber.Router{(*fiber.App)(nil), (*fiber.Group)(nil)} {
		assert.Contains(t, adaptertest.PanicMessage(t, func() { specoutfiber.New(adaptertest.Generator(), r) }),
			"specoutfiber: New needs a non-nil")
	}
}

// Fiber keeps the group prefix as written when the route path is empty, so
// group "/v1/" serves its route as "/v1/". The document must name that path.
func TestGroupPrefixSlashWithEmptyPath(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiber.New(d, app.Group("/v1/")).Get("", okRoute)
	assert.NoError(t, specoutfiber.Adopt(d, app), "a specout route is reported as a stray")
}

// Fiber copies each GET route for HEAD when it starts, and Adopt skips those
// copies. A HEAD route registered on its own is a stray like any other.
func TestAdoptReportsExplicitHeadBesideGet(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiber.New(d, app).Get("/auto", okRoute)
	specoutfiber.New(d, app).Get("/explicit", okRoute)
	app.Head("/explicit", nativeRoute)
	specoutfiber.New(d, app).Get("/both", okRoute)
	specoutfiber.New(d, app).Head("/both", okRoute)
	do(t, app, http.MethodGet, "/auto") // starting the app adds the HEAD routes
	assert.Equal(t, []string{"HEAD /explicit"}, adaptertest.Strays(t, specoutfiber.Adopt(d, app)))
}

// Middleware that answers before the endpoint still counts for the route the
// request would have reached, for GET and for the HEAD copy of GET.
func TestRecordSeesMiddlewareResponses(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	rec := recorder.New(nil)
	app.Use(specoutfiber.Record(rec))
	app.Use(func(c fiber.Ctx) error {
		if c.Query("deny") != "" {
			return fiber.ErrUnauthorized
		}
		return c.Next()
	})
	specoutfiber.New(d, app).Get("/items/:id", specout.Handler[idReq, item]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Responses:   []specout.Response{{Status: http.StatusUnauthorized}},
	})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		status, _ := do(t, app, method, "/items/1?deny=1")
		require.Equal(t, http.StatusUnauthorized, status)
	}
	do(t, app, http.MethodGet, "/items/1")
	do(t, app, http.MethodGet, "/nowhere?deny=1")
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Empty(t, ft.Errs)
}

// Fiber adds the mount prefix of a sub-app to its routes only when the app
// starts, so a Router on the sub-app documents the path without it. Record
// reports the path that ran, and Verify names the gap.
func TestRecordReportsMountedSubAppPath(t *testing.T) {
	d, app, sub := adaptertest.Generator(), fiber.New(), fiber.New()
	rec := recorder.New(nil)
	app.Use(specoutfiber.Record(rec))
	specoutfiber.New(d, sub).Get("/items/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	app.Use("/sub", sub)
	status, _ := do(t, app, http.MethodGet, "/sub/items/1")
	require.Equal(t, http.StatusNoContent, status)
	var ft adaptertest.FakeT
	recorder.Verify(&ft, d, rec)
	assert.Equal(t, []string{
		"declared GET /items/{id} 204 never produced by any test",
		"handler wrote GET /sub/items/{id} 204 but spec does not declare it",
	}, ft.Errs)
}

// Fiber tries the routes of one method in registration order, so an earlier
// route can take every request of a later one. Adopt names both routes.
func TestAdoptReportsRoutesThatNeverRun(t *testing.T) {
	accepted := specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
	}
	for _, tc := range []struct{ first, second, target string }{
		{"/users/:id", "/users/me", "/users/me"},
		{"/users", "/Users", "/Users"},
		{"/users", "/users/", "/users/"},
		{"/users/:id", "/Users/:id/", "/Users/1/"},
	} {
		d, app := adaptertest.Generator(), fiber.New()
		r := specoutfiber.New(d, app)
		r.Get(tc.first, okRoute)
		r.Get(tc.second, accepted)
		status, _ := do(t, app, http.MethodGet, tc.target)
		assert.Equal(t, http.StatusNoContent, status, "%s runs the first route", tc.target)
		require.EqualError(t, specoutfiber.Adopt(d, app), "specoutfiber: GET "+tc.second+
			" never runs: Fiber sends its requests to GET "+tc.first+", an earlier route")
	}
}

// A later route that matches requests the earlier one does not take still
// runs, and the Config can keep case or a trailing slash apart.
func TestAdoptAcceptsRoutesThatRun(t *testing.T) {
	for _, tc := range []struct {
		cfg           fiber.Config
		first, second string
	}{
		{fiber.Config{}, "/users/me", "/users/:id"},
		{fiber.Config{}, "/users/:id<int>", "/users/me"},
		{fiber.Config{}, "/files/:name", "/files/*"},
		{fiber.Config{CaseSensitive: true}, "/users", "/Users"},
		{fiber.Config{StrictRouting: true}, "/users", "/users/"},
	} {
		d, app := adaptertest.Generator(), fiber.New(tc.cfg)
		r := specoutfiber.New(d, app)
		r.Get(tc.first, okRoute)
		r.Get(tc.second, okRoute)
		require.NoError(t, specoutfiber.Adopt(d, app), "%s then %s", tc.first, tc.second)
	}
}

// A skipped guard route that calls c.Next lets the later routes run, so it
// shadows nothing.
func TestAdoptSkippedGuardShadowsNothing(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	app.All("/api/*", func(c fiber.Ctx) error { return c.Next() })
	specoutfiber.New(d, app).Get("/api/users", okRoute)
	status, _ := do(t, app, http.MethodGet, "/api/users")
	require.Equal(t, http.StatusNoContent, status, "the guard lets the route run")
	require.NoError(t, specoutfiber.Adopt(d, app, specout.Skip("/api/*")))
}
