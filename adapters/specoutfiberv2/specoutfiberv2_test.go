package specoutfiberv2_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutfiberv2"
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

// guardHint ends each "never runs" error.
const guardHint = "; if that route is a guard that calls c.Next(), skip it with specout.Skip"

func nativeRoute(c *fiber.Ctx) error {
	c.Status(http.StatusNoContent)
	return nil
}

func TestGroupRouteDocumentedAtFullPath(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	g := app.Group("/api").Group("/v1")
	specoutfiberv2.New(d, g).Get("/users/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
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
		{"/blobs/+", "/blobs/x/y", "+1", "x/y"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			specoutfiberv2.New(d, app).Get(tc.route, pathValueRoute(tc.name))
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
		specoutfiberv2.New(d, app).Get("/users/:id", pathValueRoute("id"))
		status, header := do(t, app, http.MethodGet, tc.target)
		require.Equal(t, http.StatusNoContent, status)
		assert.Equal(t, tc.want, header.Get("X-Value"), tc.target)
	}
}

func TestRouteMiddlewareRunsFirst(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	var order []string
	mw := func(c *fiber.Ctx) error {
		order = append(order, "middleware")
		return c.Next()
	}
	specoutfiberv2.New(d, app).Get("/x", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
			w.WriteHeader(http.StatusNoContent)
		},
	}, mw)
	status, _ := do(t, app, http.MethodGet, "/x")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, []string{"middleware", "handler"}, order)
}

// Fiber v2 adds HEAD only through its own Get, at registration. A specout GET
// route goes through Get, so it answers HEAD; a POST route does not.
func TestGetRouteAnswersHead(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	r := specoutfiberv2.New(d, app)
	r.Get("/x", okRoute)
	r.Post("/y", okRoute)
	status, _ := do(t, app, http.MethodHead, "/x")
	assert.Equal(t, http.StatusNoContent, status)
	status, _ = do(t, app, http.MethodHead, "/y")
	assert.Equal(t, http.StatusMethodNotAllowed, status)
	assert.NotContains(t, adaptertest.Paths(t, d)["/x"], "head", "the document lists the GET operation only")
}

func TestPathTranslation(t *testing.T) {
	for _, tc := range []struct {
		route string
		want  string // "" for a catch-all, which the document omits
	}{
		{"/users/:id", "/users/{id}"},
		{"/users/:id<int>", "/users/{id}"},
		{"/x/:id<int;min(1)>", "/x/{id}"},
		{`/r/:id<regex(\d+)>`, "/r/{id}"},
		{"/flights/:from-:to", "/flights/{from}-{to}"},
		{"/:a-:b.:c", "/{a}-{b}.{c}"},
		{"/v1/:id.json", "/v1/{id}.json"},
		{"/p/:a/:b", "/p/{a}/{b}"},
		{"/a:b", "/a{b}"},
		{"/color::color", "/color:{color}"},
		{"/shop/color::color/size::size", "/shop/color:{color}/size:{size}"},
		{`/a\:b`, "/a:b"},
		{`/esc/\*`, "/esc/*"},
		{"/files/*", ""},
		{"/files/+", ""},
		{"/a/*/b/*", ""},
		{"/api/:param/abc/*", ""},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			specoutfiberv2.New(d, app).Get(tc.route, okRoute)
			paths := adaptertest.Paths(t, d)
			if tc.want == "" {
				assert.Empty(t, paths)
			} else {
				assert.Contains(t, paths, tc.want)
			}
			// Adopt compares the parameter names with the ones Fiber parsed.
			assert.NoError(t, specoutfiberv2.Adopt(d, app), "a specout route counts as documented")
		})
	}
}

func TestRegistrationPanicLeavesNoRecord(t *testing.T) {
	for _, tc := range []struct{ route, want string }{
		{"/a/{b}", "specoutfiberv2: GET /a/{b}: literal braces"},
		{"/users/:id?", "specoutfiberv2: GET /users/:id?: optional parameter :id?"},
		{"/plus/:name+", "specoutfiberv2: GET /plus/:name+: parameter :name is followed by '+'"},
		{"/:param*", "specoutfiberv2: GET /:param*: parameter :param is followed by '*'"},
		{`/:id<regex(\d{4})>\:x`, `a Fiber v2 bug skips the \ after the constraint`},
		{`/v1/:id<regex(\d)>\:x`, `a Fiber v2 bug skips the \ after the constraint`},
		{`/:id<regex(a.b)>.x`, "a Fiber v2 bug skips the . after the constraint"},
		{`/:id<regex(a-b)>-x`, "a Fiber v2 bug skips the - after the constraint"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New()
			r := specoutfiberv2.New(d, app)
			r.Get("/ok", okRoute)
			assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get(tc.route, okRoute) }), tc.want)
			assert.Len(t, adaptertest.Paths(t, d), 1)
			assert.NoError(t, specoutfiberv2.Adopt(d, app))
		})
	}
}

// Fiber v2 appends a second route on the same path to the first, so the
// second handler would never run. Bind fails on it instead.
func TestDuplicateRoutePanics(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	r := specoutfiberv2.New(d, app)
	r.Get("/x", okRoute)
	assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get("/x", okRoute) }), "duplicate route GET /x")
}

func TestAdoptReportsStrays(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	app.Use(func(c *fiber.Ctx) error { return c.Next() })
	specoutfiberv2.New(d, app).Get("/documented", okRoute)
	app.Get("/plain", nativeRoute)
	app.Get("/openapi.json", nativeRoute)
	got := adaptertest.Strays(t, specoutfiberv2.Adopt(d, app, specout.Skip("/openapi.json")))
	assert.Equal(t, []string{"GET /plain"}, got)
}

func TestRequireDocumented(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiberv2.New(d, app).Get("/documented", okRoute)
	var pass adaptertest.FakeT
	specoutfiberv2.RequireDocumented(&pass, d, app)
	assert.Empty(t, pass.Errs)

	app.Get("/plain", nativeRoute)
	var fail adaptertest.FakeT
	specoutfiberv2.RequireDocumented(&fail, d, app)
	require.Len(t, fail.Errs, 1)
	assert.Contains(t, fail.Errs[0], "GET /plain")
}

// recorded registers GET /items/:id, declaring 200 and 404, behind Record.
func recorded(t *testing.T) (*specout.Generator, *fiber.App, *recorder.Recorder) {
	t.Helper()
	d, app := adaptertest.Generator(), fiber.New()
	rec := recorder.New(nil, specout.Skip("/openapi.json"))
	app.Use(specoutfiberv2.Record(rec))
	specoutfiberv2.New(d, app).Get("/items/:id", specout.Handler[idReq, item]{
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

// otherRouter is a fiber.Router that is neither an app nor a group.
type otherRouter struct{ fiber.Router }

func TestNewPanics(t *testing.T) {
	app := fiber.New()
	assert.Panics(t, func() { specoutfiberv2.New(nil, app) })
	assert.Panics(t, func() { specoutfiberv2.New(adaptertest.Generator(), nil) })
	assert.Contains(t, adaptertest.PanicMessage(t, func() { specoutfiberv2.New(adaptertest.Generator(), otherRouter{app}) }),
		"specoutfiberv2: New needs a non-nil *fiber.App or *fiber.Group")
	for _, r := range []fiber.Router{(*fiber.App)(nil), (*fiber.Group)(nil)} {
		assert.Contains(t, adaptertest.PanicMessage(t, func() { specoutfiberv2.New(adaptertest.Generator(), r) }),
			"specoutfiberv2: New needs a non-nil")
	}
}

// Fiber keeps the group prefix as written when the route path is empty, so
// group "/v1/" serves its route as "/v1/". The document must name that path.
func TestGroupPrefixSlashWithEmptyPath(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiberv2.New(d, app.Group("/v1/")).Get("", okRoute)
	assert.NoError(t, specoutfiberv2.Adopt(d, app), "a specout route is reported as a stray")
}

// Fiber's Get adds a HEAD route that shares the GET route's handlers, and
// Adopt skips it. A HEAD route registered on its own is a stray like any
// other, and one that specout registered is documented.
func TestAdoptReportsExplicitHeadBesideGet(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiberv2.New(d, app).Get("/auto", okRoute)
	specoutfiberv2.New(d, app).Get("/explicit", okRoute)
	app.Head("/explicit", nativeRoute)
	specoutfiberv2.New(d, app).Head("/both", okRoute)
	specoutfiberv2.New(d, app).Get("/both", okRoute)
	do(t, app, http.MethodGet, "/auto") // starting the app adds no HEAD routes in v2
	assert.Equal(t, []string{"HEAD /explicit"}, adaptertest.Strays(t, specoutfiberv2.Adopt(d, app)))
}

// A Head route registered before the Get route on its path runs for HEAD.
func TestHeadBeforeGetRunsForHead(t *testing.T) {
	late := specoutfiberv2.New(adaptertest.Generator(), fiber.New())
	late.Get("/x", okRoute)
	assert.Contains(t, adaptertest.PanicMessage(t, func() { late.Head("/x", okRoute) }), "register Head before Get")

	d, app := adaptertest.Generator(), fiber.New()
	r := specoutfiberv2.New(d, app)
	r.Head("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Route", "head")
		w.WriteHeader(http.StatusNoContent)
	}})
	r.Get("/x", okRoute)
	status, header := do(t, app, http.MethodHead, "/x")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, "head", header.Get("X-Route"))
}

// The HEAD route that Fiber's Get adds takes the HEAD requests of a later
// Head route that it matches, so Adopt reports that route.
func TestAdoptReportsHeadAfterMatchingGet(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	r := specoutfiberv2.New(d, app)
	r.Get("/users/:id", okRoute)
	r.Head("/users/me", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) },
	})
	status, _ := do(t, app, http.MethodHead, "/users/me")
	assert.Equal(t, http.StatusNoContent, status, "the HEAD route of GET /users/:id runs")
	require.EqualError(t, specoutfiberv2.Adopt(d, app), "specoutfiberv2: HEAD /users/me never runs: "+
		"Fiber sends its requests to HEAD /users/:id, the HEAD route that an earlier Get adds"+guardHint)
}

// deniedApp serves GET /items/:id, declaring 401, behind Record and middleware
// that answers 401 for ?deny=1 before the endpoint runs.
func deniedApp() (*specout.Generator, *fiber.App, *recorder.Recorder) {
	d, app := adaptertest.Generator(), fiber.New()
	rec := recorder.New(nil)
	app.Use(specoutfiberv2.Record(rec))
	app.Use(func(c *fiber.Ctx) error {
		if c.Query("deny") != "" {
			return fiber.ErrUnauthorized
		}
		return c.Next()
	})
	specoutfiberv2.New(d, app).Get("/items/:id", specout.Handler[idReq, item]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Responses:   []specout.Response{{Status: http.StatusUnauthorized}},
	})
	return d, app, rec
}

// Middleware that answers before the endpoint still counts for the route the
// request would have reached, for GET and for the HEAD route of GET.
func TestRecordSeesMiddlewareResponses(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			d, app, rec := deniedApp()
			status, _ := do(t, app, method, "/items/1?deny=1")
			require.Equal(t, http.StatusUnauthorized, status)
			do(t, app, http.MethodGet, "/items/1")
			do(t, app, http.MethodGet, "/nowhere?deny=1")
			var ft adaptertest.FakeT
			recorder.Verify(&ft, d, rec)
			assert.Empty(t, ft.Errs)
		})
	}
}

// Fiber serves /items/1/ on /items/:id unless StrictRouting is set, but
// fiber.RoutePatternMatch keeps the request's trailing slash. Record drops it.
func TestRecordMiddlewareResponseWithTrailingSlash(t *testing.T) {
	d, app, rec := deniedApp()
	status, _ := do(t, app, http.MethodGet, "/items/1/?deny=1")
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = do(t, app, http.MethodGet, "/items/1/")
	require.Equal(t, http.StatusOK, status)
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
	app.Use(specoutfiberv2.Record(rec))
	specoutfiberv2.New(d, sub).Get("/items/:id", specout.Handler[idReq, specout.NoContent]{HandlerFunc: noContent})
	app.Mount("/sub", sub)
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
		{"/files/*", "/files/:name", "/files/a"},
		{"/:a/:b", "/users/:id", "/users/1"},
	} {
		d, app := adaptertest.Generator(), fiber.New()
		r := specoutfiberv2.New(d, app)
		r.Get(tc.first, okRoute)
		r.Get(tc.second, accepted)
		status, _ := do(t, app, http.MethodGet, tc.target)
		assert.Equal(t, http.StatusNoContent, status, "%s runs the first route", tc.target)
		require.EqualError(t, specoutfiberv2.Adopt(d, app), "specoutfiberv2: GET "+tc.second+
			" never runs: Fiber sends its requests to GET "+tc.first+", an earlier route"+guardHint)
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
		{fiber.Config{}, "/users/:id<int>", "/users/:name"},
		{fiber.Config{}, "/:a/:b", "/users/:id/edit"},
		{fiber.Config{}, `/a/\:id`, "/a/:id"},
		{fiber.Config{CaseSensitive: true}, "/users", "/Users"},
		{fiber.Config{StrictRouting: true}, "/users", "/users/"},
	} {
		d, app := adaptertest.Generator(), fiber.New(tc.cfg)
		r := specoutfiberv2.New(d, app)
		r.Get(tc.first, okRoute)
		r.Get(tc.second, okRoute)
		require.NoError(t, specoutfiberv2.Adopt(d, app), "%s then %s", tc.first, tc.second)
	}
}

// Fiber v2 reads an escaped colon right after a constraint without a
// backslash as a literal, so the route serves /1001:x.
func TestRegexConstraintBeforeEscapedColon(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiberv2.New(d, app).Get(`/:id<regex([0-9]{4})>\:x`, pathValueRoute("id"))
	status, header := do(t, app, http.MethodGet, "/1001:x")
	require.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, "1001", header.Get("X-Value"))
	assert.Contains(t, adaptertest.Paths(t, d), "/{id}:x")
	require.NoError(t, specoutfiberv2.Adopt(d, app))
}

// A constraint that writes a dot as \x2e does not hold the . after it, so
// Fiber v2 ends the parameter at that dot.
func TestRegexConstraintWithoutTheDelimiter(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	specoutfiberv2.New(d, app).Get(`/:id<regex(^a\x2eb$)>.x`, pathValueRoute("id"))
	status, header := do(t, app, http.MethodGet, "/a.b.x")
	require.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, "a.b", header.Get("X-Value"))
	status, _ = do(t, app, http.MethodGet, "/a.b")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, adaptertest.Paths(t, d), "/{id}.x")
}

// A skipped guard route that calls c.Next lets the later routes run, so it
// shadows nothing.
func TestAdoptSkippedGuardShadowsNothing(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	app.All("/api/*", func(c *fiber.Ctx) error { return c.Next() })
	specoutfiberv2.New(d, app).Get("/api/users", okRoute)
	status, _ := do(t, app, http.MethodGet, "/api/users")
	require.Equal(t, http.StatusNoContent, status, "the guard lets the route run")
	require.NoError(t, specoutfiberv2.Adopt(d, app, specout.Skip("/api/*")))
}

// Bind checks a route after Fiber adds it. A registration that panics there
// serves nothing, even when the caller recovers: the route passes its
// requests on to the next route.
func TestPanickedRegistrationServesNothing(t *testing.T) {
	d, app := adaptertest.Generator(), fiber.New()
	r := specoutfiberv2.New(d, app)
	r.Get("/users/:id<int>", okRoute)
	teapot := specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) },
	}
	assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get("/users/:id", teapot) }), "duplicate route")
	assert.Contains(t, adaptertest.PanicMessage(t, func() { r.Get("/a//b", teapot) }), "empty segment")
	for _, target := range []string{"/users/abc", "/a//b"} {
		status, _ := do(t, app, http.MethodGet, target)
		assert.Equal(t, http.StatusNotFound, status, target)
	}
	status, _ := do(t, app, http.MethodGet, "/users/1")
	assert.Equal(t, http.StatusNoContent, status, "the first route still runs")
}

// A net/http handler that hijacks the connection writes the response itself,
// and Fiber keeps its default 200. Record used to report that 200 for each
// case of adaptertest.HijackCases. fasthttp's adaptor cannot hijack a
// response that it already streams, so the flushed case does not run.
func TestRecordHijackedResponses(t *testing.T) {
	for _, tc := range adaptertest.HijackCases() {
		if tc.Flushed {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			d, app := adaptertest.Generator(), fiber.New(fiber.Config{DisableStartupMessage: true})
			recorded := make(chan struct{}, 1)
			app.Use(func(c *fiber.Ctx) error {
				defer func() { recorded <- struct{}{} }()
				return c.Next()
			})
			rec := recorder.New(nil)
			app.Use(specoutfiberv2.Record(rec))
			specoutfiberv2.New(d, app).Get("/ws", tc.Route())
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go func() { _ = app.Listener(ln) }()
			t.Cleanup(func() { _ = app.Shutdown() })
			assert.Equal(t, tc.Status, adaptertest.RawGet(t, ln.Addr().String(), "/ws", tc.Upgrade))
			<-recorded
			var ft adaptertest.FakeT
			recorder.Verify(&ft, d, rec)
			assert.Equal(t, tc.Errs, ft.Errs)
		})
	}
}
