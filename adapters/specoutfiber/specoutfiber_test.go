package specoutfiber_test

import (
	"net"
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
	specout.Bind(d, specoutfiber.New(g)).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
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
			specout.Bind(d, specoutfiber.New(app)).Get(tc.route, pathValueRoute(tc.name))
			status, header := do(t, app, http.MethodGet, tc.target)
			require.Equal(t, http.StatusNoContent, status)
			assert.Equal(t, tc.want, header.Get("X-Value"))
		})
	}
}

func pathValueRoute(name string) specout.Handler[struct{}, specout.NoContent] {
	return specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Value", r.PathValue(name))
			w.WriteHeader(http.StatusNoContent)
		},
	}
}

func TestHandlerSeesFiberPathValues(t *testing.T) {
	for _, tc := range []struct {
		cfg          fiber.Config
		target, want string
	}{
		{fiber.Config{}, "/users/a%40b", "a%40b"},
		{fiber.Config{UnescapePath: true}, "/users/a%40b", "a@b"},
	} {
		d, app := adaptertest.Generator(), fiber.New(tc.cfg)
		specout.Bind(d, specoutfiber.New(app)).Get("/users/:id", pathValueRoute("id"))
		status, header := do(t, app, http.MethodGet, tc.target)
		require.Equal(t, http.StatusNoContent, status)
		assert.Equal(t, tc.want, header.Get("X-Value"), tc.target)
	}
}

func TestGroupMiddlewareRunsFirst(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	var order []string
	mw := func(c fiber.Ctx) error {
		order = append(order, "middleware")
		return c.Next()
	}
	specout.Bind(d, specoutfiber.New(app.Group("/g", mw))).Get("/x", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
			w.WriteHeader(http.StatusNoContent)
		},
	})
	status, _ := do(t, app, http.MethodGet, "/g/x")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, []string{"middleware", "handler"}, order)
}

func TestPathTranslation(t *testing.T) {
	for _, tc := range []struct {
		route string
		want  string
	}{
		{"/users/:id<int>", "/users/{id}"},
		{"/flights/:from-:to", "/flights/{from}-{to}"},
		{"/files/*", ""},
		{`/a\:b`, "/a:b"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			specout.Bind(d, specoutfiber.New(app)).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			assert.NoError(t, specout.Check(d, specoutfiber.New(app)), "a specout route counts as documented")
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
			r := specout.Bind(d, specoutfiber.New(app))
			r.Get("/ok", okRoute)
			assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get(tc.route, okRoute) }), tc.want)
			assert.Len(t, adaptertest.Paths(t, d), 1)
			assert.NoError(t, specout.Check(d, specoutfiber.New(app)))
		})
	}
}

func TestCheckReportsStrays(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	app.Use(func(c fiber.Ctx) error { return c.Next() })
	specout.Bind(d, specoutfiber.New(app)).Get("/documented", okRoute)
	app.Get("/plain", nativeRoute)
	app.Get("/openapi.json", nativeRoute)
	do(t, app, http.MethodGet, "/documented")
	got := adaptertest.Strays(t, specout.Check(d, specoutfiber.New(app), "/openapi.json"))
	assert.Equal(t, []string{"GET /plain"}, got)
}

func recorded(t *testing.T) (*specout.Generator, *fiber.App, *recorder.Recorder) {
	t.Helper()
	d, app := adaptertest.Generator(), fiber.New()
	rec := recorder.New(nil, "/openapi.json")
	app.Use(specoutfiber.Record(rec))
	specout.Bind(d, specoutfiber.New(app)).Get("/items/:id", specout.Handler[idReq, item]{
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
	assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecordReportsMissingStatus(t *testing.T) {
	d, app, rec := recorded(t)
	do(t, app, http.MethodGet, "/items/1")
	assert.Equal(t, []string{"declared GET /items/{id} 404 never produced by any test"}, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecordHeadCountsForGet(t *testing.T) {
	d, app, rec := recorded(t)
	do(t, app, http.MethodHead, "/items/1")
	do(t, app, http.MethodHead, "/items/missing")
	assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecorderWithoutRouterPanics(t *testing.T) {
	rec := recorder.New(nil)
	assert.Panics(t, func() {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	})
}

func TestNewPanics(t *testing.T) {
	app := fiber.New()
	assert.Panics(t, func() { specout.Bind(nil, specoutfiber.New(app)) })
	assert.Panics(t, func() { specout.Bind(adaptertest.Generator(), specoutfiber.New(nil)) })
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specout.Bind(adaptertest.Generator(), specoutfiber.New(app.Domain("x.com"))) }),
		"specoutfiber: New needs a non-nil *fiber.App or *fiber.Group")
	for _, r := range []fiber.Router{(*fiber.App)(nil), (*fiber.Group)(nil)} {
		assert.Contains(t, adaptertest.PanicMessage(t, func() { specout.Bind(adaptertest.Generator(), specoutfiber.New(r)) }),
			"specoutfiber: New needs a non-nil")
	}
}

func TestGroupPrefixSlashWithEmptyPath(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specout.Bind(d, specoutfiber.New(app.Group("/v1/"))).Get("", okRoute)
	assert.NoError(t, specout.Check(d, specoutfiber.New(app)), "a specout route is reported as a stray")
}

func TestCheckReportsExplicitHeadBesideGet(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specout.Bind(d, specoutfiber.New(app)).Get("/auto", okRoute)
	specout.Bind(d, specoutfiber.New(app)).Get("/explicit", okRoute)
	app.Head("/explicit", nativeRoute)
	specout.Bind(d, specoutfiber.New(app)).Get("/both", okRoute)
	specout.Bind(d, specoutfiber.New(app)).Head("/both", okRoute)
	do(t, app, http.MethodGet, "/auto")
	assert.Equal(t, []string{"HEAD /explicit"}, adaptertest.Strays(t, specout.Check(d, specoutfiber.New(app))))
}

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
	specout.Bind(d, specoutfiber.New(app)).Get("/items/:id", specout.Handler[idReq, item]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Responses:   []specout.Response{{Status: http.StatusUnauthorized}},
	})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		status, _ := do(t, app, method, "/items/1?deny=1")
		require.Equal(t, http.StatusUnauthorized, status)
	}
	do(t, app, http.MethodGet, "/items/1")
	do(t, app, http.MethodGet, "/nowhere?deny=1")
	assert.Empty(t, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestRecordReportsMountedSubAppPath(t *testing.T) {
	d, app, sub := adaptertest.Generator(), fiber.New(), fiber.New()
	rec := recorder.New(nil)
	app.Use(specoutfiber.Record(rec))
	specout.Bind(d, specoutfiber.New(sub)).Get("/items/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	app.Use("/sub", sub)
	status, _ := do(t, app, http.MethodGet, "/sub/items/1")
	require.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, []string{
		"declared GET /items/{id} 204 never produced by any test",
		"handler wrote GET /sub/items/{id} 204 but spec does not declare it",
	}, adaptertest.Errs(recorder.Verify(d, rec)))
}

func TestBadRegistrationPanics(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	r := specout.Bind(d, specoutfiber.New(app))
	r.Get("/users/:id<int>", okRoute)
	teapot := specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) },
	}
	assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get("/users/:id", teapot) }), "duplicate route")
	assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get("/a//b", teapot) }), "empty segment")
}

func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		if tc.Flushed {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			recorded := make(chan struct{}, 1)
			app.Use(func(c fiber.Ctx) error {
				defer func() { recorded <- struct{}{} }()
				return c.Next()
			})
			rec := recorder.New(nil)
			app.Use(specoutfiber.Record(rec))
			specout.Bind(d, specoutfiber.New(app)).Get("/ws", tc.Route())
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
			t.Cleanup(func() { _ = app.Shutdown() })
			assert.Equal(t, tc.Status, adaptertest.RawGet(t, ln.Addr().String(), "/ws", tc.Upgrade))
			<-recorded
			assert.Equal(t, tc.Errs, adaptertest.Errs(recorder.Verify(d, rec)))
		})
	}
}
