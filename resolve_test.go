package specout_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	gmux "github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Registration after one build still resolves on the next build: the
// resolver is not a one-shot latch (freeze still applies after serve).
func TestLateRegistrationResolves(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/early", okGet)
	adopt(t, rc)
	// no build yet; register a second route, then build once
	rc.Get("/late", okGet)
	declared := declaredStatuses(t, d)
	require.Contains(t, declared, specout.RouteKey{Method: "GET", Path: "/late"}, "late route unresolved before any build")
	assert.Contains(t, docPaths(t, d), "/late", "late route missing")
}

// Missing adopt: relative pattern has no composed full path -> build error.
func TestUnresolvedPatternFails(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		specout.Chi(d, r).Get("/x", okGet)
	})
	// no Adopt of the root; the inner registration is relative
	wantBuildErr(t, d, "never resolved")
}

// One func on two chi routes pairs deterministically across two builds.
func TestSharedHandlerPairingStable(t *testing.T) {
	build := func() []string {
		d, r := newGen(), chi.NewRouter()
		rc := specout.Chi(d, r)
		rc.Get("/a/{id}", okGet)
		rc.Get("/b/{id}", okGet)
		adopt(t, rc)
		return keys(docPaths(t, d))
	}
	first, second := build(), build()
	require.Equal(t, first, second, "pairing unstable across two builds")
	require.Len(t, first, 2, "want 2 paths")
}

// chi /a and /a/ are distinct walk patterns; their derived operationIds
// collide, so the build demands an explicit OperationID on one of them.
func TestChiSlashVariantsDistinct(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/a", okGet)
	rc.Get("/a/", okGet)
	adopt(t, rc)
	wantBuildErr(t, d, "operationId")
}

// Catch-alls are omitted from paths but stay in DeclaredStatuses.
func TestCatchAllOmittedButDeclared(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/files/*", okGet)
	adopt(t, rc)
	assert.NotContains(t, docPaths(t, d), "/files/*", "catch-all leaked into paths")
	assert.Contains(t, declaredStatuses(t, d), specout.RouteKey{Method: "GET", Path: "/files/*"}, "catch-all missing from drift keys")
}

// Two views of the drift map: DeclaredStatuses is what the route declares for
// itself, SpecStatuses adds the global DefaultErrors envelope, exactly as the
// emitted spec stamps it. The recorder uses both; a handler returning a
// declared default is not drift.
func TestSpecStatusesIncludesDefaults(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1",
		ErrorType: Problem{}, DefaultErrors: []int{400, 404, 500},
	})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/x", okGet)
	adopt(t, rc)

	key := specout.RouteKey{Method: "GET", Path: "/x"}
	decl := declaredStatuses(t, d)
	assert.False(t, decl[key][404], "DeclaredStatuses carries the global envelope: %v", decl[key])
	assert.True(t, decl[key][204], "DeclaredStatuses missing the route's own 204: %v", decl[key])
	spec := specStatuses(t, d)
	for _, c := range []int{204, 400, 404, 500} {
		assert.True(t, spec[key][c], "SpecStatuses missing %d: %v", c, spec[key])
	}

	// Omit removes a global default from one route only (both views).
	rc.Get("/y", noBody{HandlerFunc: okBody, Responses: []specout.Response{{Status: 404, Omit: true}}})
	key = specout.RouteKey{Method: "GET", Path: "/y"}
	decl, spec = declaredStatuses(t, d), specStatuses(t, d)
	assert.False(t, decl[key][404] || spec[key][404], "Omit did not remove 404: decl=%v spec=%v", decl[key], spec[key])
	assert.True(t, spec[key][500], "sibling default 500 dropped: %v", spec[key])
}

// Duplicate canonical (path, method) from two registrations fails loud:
// chi /x and /x/ are distinct (both kept); identical paths are the error.
func TestDuplicateCanonicalFails(t *testing.T) {
	d := newGen()
	gc := specout.Gorilla(d, gmux.NewRouter())
	gc.Get("/x", okGet)
	gc.Get("/x", noBody{HandlerFunc: okBody})
	wantBuildErr(t, d, "duplicate")
}

// Gorilla: verbs, path params, template match.
func TestGorillaAdapter(t *testing.T) {
	d, g := newGen(), gmux.NewRouter()
	gc := specout.Gorilla(d, g)
	gc.Get("/items/{id}", okGet)
	gc.Post("/items", okGet)
	assert.NotNil(t, docPaths(t, d)["/items/{id}"], "/items/{id} missing")
	// live serve check
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/9", nil))
	require.Equal(t, 204, w.Code, "gorilla serve")
}

// Gorilla Adopt: a plain gorilla route is a stray; a skip suppresses it.
func TestGorillaAdoptStrayAndSkip(t *testing.T) {
	d, g := newGen(), gmux.NewRouter()
	specout.Gorilla(d, g).Get("/ok", okGet)
	g.HandleFunc("/stray", strayHandler).Methods("GET")
	require.ErrorContains(t, specout.Gorilla(d, g).Adopt(), "/stray")
	require.NoError(t, specout.Gorilla(d, g).Adopt(specout.Skip("/stray")), "skip should suppress")
}

// Gorilla Adopt: method-less route fails loud instead of silently skipping.
func TestGorillaMethodlessFails(t *testing.T) {
	g := gmux.NewRouter()
	g.HandleFunc("/x", okBody)
	require.ErrorContains(t, specout.Gorilla(newGen(), g).Adopt(), "no method constraint")
}

// Document escape hatch records absolute patterns for foreign routers.
func TestDocument(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/anything", okGet)
	require.Contains(t, docPaths(t, d), "/anything", "documented route missing")
}

// Document is the whole integration for a router with no adapter: it records
// an absolute pattern with no Adopt, and the foreign mux still serves it.
func TestDocumentForeignRouterEndToEnd(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	specout.Document(d, http.MethodGet, "/echo/items/{id}", okGet)
	mux.HandleFunc("GET /echo/items/{id}", okGet.HandlerFunc)
	require.Contains(t, docPaths(t, d), "/echo/items/{id}", "Document path missing from spec")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/echo/items/7", nil))
	assert.Equal(t, 204, rec.Code, "foreign mux serve")
}

// Bad method tokens and relative std patterns panic (std Handle and Document).
func TestBadMethodPanics(t *testing.T) {
	mux := http.NewServeMux()
	for _, bad := range []string{"get /x", "GET  /x", "GET x"} {
		require.Panics(t, func() { specout.Std(newGen(), mux).Handle(bad, okGet) })
	}
	require.Panics(t, func() { specout.Document(newGen(), "get", "/x", okGet) })
	require.Panics(t, func() { specout.Document(newGen(), "GET", "x", okGet) })
}

// Gorilla composes subrouter prefixes into the route template, so the spec
// path is the one the server actually serves.
func TestGorillaSubrouterPrefixComposed(t *testing.T) {
	d, root := newGen(), gmux.NewRouter()
	specout.Gorilla(d, root.PathPrefix("/api").Subrouter()).Get("/items", okGet)
	require.Contains(t, docPaths(t, d), "/api/items", "prefix lost")
	w := httptest.NewRecorder()
	root.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/items", nil))
	require.Equal(t, 204, w.Code, "serve")
}

// A std multi-segment wildcard is a catch-all: kept for drift, out of paths.
func TestStdMultiSegmentWildcardOmitted(t *testing.T) {
	d := newGen()
	specout.Std(d, http.NewServeMux()).Handle("GET /files/{path...}", okGet)
	assert.NotContains(t, docPaths(t, d), "/files/{path...}", "std {path...} leaked into paths")
	assert.Contains(t, declaredStatuses(t, d), specout.RouteKey{Method: "GET", Path: "/files/{path...}"}, "catch-all missing from drift keys")
}

// One func on a std mux and a chi router keeps both paths: the absolute
// record is not overwritten by the chi walk.
func TestStdAndChiShareFunc(t *testing.T) {
	d := newGen()
	specout.Std(d, http.NewServeMux()).Handle("GET /std", okGet)
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/chi", okGet)
	adopt(t, rc)
	paths := docPaths(t, d)
	for _, p := range []string{"/std", "/chi"} {
		assert.Contains(t, paths, p, "%s missing", p)
	}
}

// Adopt reports strays without breaking resolution: the walk source is
// registered even when the router holds undocumented routes.
func TestAdoptStraysStillResolve(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/ok", okGet)
	r.Get("/stray", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	require.ErrorContains(t, rc.Adopt(), "/stray")
	require.Contains(t, docPaths(t, d), "/ok", "documented route lost to the stray")
}

// A shared handler keeps its own metadata: pairing never swaps /a and /b/c.
func TestSharedHandlerMetadataNotSwapped(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	rc := specout.Chi(d, r)
	h := noBody{HandlerFunc: okBody, Summary: "SHORT"}
	rc.Get("/a", h)
	h.Summary = "LONG"
	rc.Get("/b/c", h)
	adopt(t, rc)
	for path, summary := range map[string]string{"/a": "SHORT", "/b/c": "LONG"} {
		assert.Equal(t, summary, opOf(t, buildDoc(t, d), path, "get")["summary"], path)
	}
}

// One route reachable at several paths it never registered is a build
// error naming every path: documenting one would silently drop the rest.
func TestDualMountFailsLoud(t *testing.T) {
	d, root, sub := newGen(), chi.NewRouter(), chi.NewRouter()
	specout.Chi(d, sub).Get("/items", okGet)
	root.Mount("/v1", sub)
	root.Mount("/v2", sub)
	adopt(t, specout.Chi(d, root))
	wantBuildErr(t, d, "/v2/items")
}

// Adopting a subrouter and its parent registers two walk sources; the
// relative view is not a second mount point, so this fails loud too.
func TestSubAndRootAdoptedFailsLoud(t *testing.T) {
	d, root, sub := newGen(), chi.NewRouter(), chi.NewRouter()
	specout.Chi(d, sub).Get("/items", okGet)
	root.Mount("/api", sub)
	adopt(t, specout.Chi(d, sub))
	adopt(t, specout.Chi(d, root))
	wantBuildErr(t, d, "outermost")
}

// Router regex constraints are not OpenAPI templates: /items/{id:[0-9]+}
// documents as /items/{id}, and the derived operationId stays a Go name.
func TestRegexParamPathNormalized(t *testing.T) {
	d := newGen()
	specout.Gorilla(d, gmux.NewRouter()).Get("/items/{id:[0-9]+}", okGet)
	paths := docPaths(t, d)
	op, ok := paths["/items/{id}"].(map[string]any)
	require.True(t, ok, "regex constraint leaked into the path")
	assert.Equal(t, "getItemsId", op["get"].(map[string]any)["operationId"])
}

// A gorilla PathPrefix Subrouter parent has no handler and is a mount, not
// an endpoint: Adopt must not flag it.
func TestGorillaSubrouterAdoptClean(t *testing.T) {
	d, root := newGen(), gmux.NewRouter()
	specout.Gorilla(d, root.PathPrefix("/api").Subrouter()).Get("/items", okGet)
	require.NoError(t, specout.Gorilla(d, root).Adopt(), "subrouter parent flagged as stray")
}

// A brace regex quantifier ({4}) nests braces inside the constraint. It must
// not leak a stray brace into the documented path or the operationId.
func TestBraceQuantifierParamNormalized(t *testing.T) {
	const pat = "/x/{id:[0-9]{4}}"
	check := func(name string, d *specout.Generator) {
		t.Helper()
		op, ok := docPaths(t, d)["/x/{id}"].(map[string]any)
		if !assert.True(t, ok, "%s: want /x/{id}", name) {
			return
		}
		assert.Equal(t, "getXId", op["get"].(map[string]any)["operationId"], name)
	}
	d := newGen()
	specout.Gorilla(d, gmux.NewRouter()).Get(pat, okGet)
	check("gorilla", d)
	d, r := newGen(), chi.NewRouter()
	specout.Chi(d, r).Get(pat, okGet)
	adopt(t, specout.Chi(d, r))
	check("chi", d)
	d = newGen()
	specout.Document(d, http.MethodGet, pat, okGet)
	check("document", d)
}

// Two routes with different regex but one documented path are a real
// collision: the second would silently overwrite the first.
func TestDocPathCollisionFails(t *testing.T) {
	d := newGen()
	gc := specout.Gorilla(d, gmux.NewRouter())
	gc.Get("/x/{id:[0-9]+}", noBody{HandlerFunc: okBody, OperationID: "alpha"})
	gc.Get("/x/{id:[a-z]+}", noBody{HandlerFunc: okBody, OperationID: "beta"})
	wantBuildErr(t, d, "/x/{id}")
}

// Serving the spec from the same gorilla router it documents is a mount, not
// an endpoint: Adopt must stay quiet.
func TestGorillaSpecMountNotStray(t *testing.T) {
	d, g := newGen(), gmux.NewRouter()
	specout.Gorilla(d, g).Get("/ok", okGet)
	g.Handle("/openapi.json", d)
	require.NoError(t, specout.Gorilla(d, g).Adopt(), "spec mount flagged as stray")
}

// A zero Handler has no func: the binder must refuse it at registration
// instead of documenting an endpoint that panics when served.
func TestNilHandlerPanics(t *testing.T) {
	require.Panics(t, func() {
		specout.Gorilla(newGen(), gmux.NewRouter()).Get("/x",
			noBody{Summary: "documents a segfault"})
	})
}

// A method OpenAPI cannot name (CONNECT) would emit an invalid path-item
// key, so the binder panics instead of writing an invalid document.
func TestNonOpenAPIMethodPanics(t *testing.T) {
	require.Panics(t, func() { specout.Document(newGen(), "CONNECT", "/x", okGet) })
	require.Panics(t, func() { specout.Std(newGen(), http.NewServeMux()).Handle("CONNECT /x", okGet) })
}

// A repeated path param name (/x/{id}/y/{id}) is one OpenAPI parameter:
// OpenAPI forbids two entry names in one operation.
func TestDuplicatePathParamEmittedOnce(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x/{id}/y/{id}", okGet)
	params, _ := opOf(t, buildDoc(t, d), "/x/{id}/y/{id}", "get")["parameters"].([]any)
	require.Len(t, params, 1, "want 1 path param")
	assert.Equal(t, "id", params[0].(map[string]any)["name"])
}

// A path:"name" tag with no {name} in the pattern is a typo: the field leaves
// the body and the placeholder stays a plain string, so the document would be
// quietly wrong. The mismatched name is the case a build must reject.
type strayPathReq struct {
	ID string `path:"Id"`
}

func TestStrayPathFieldFails(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x/{id}", specout.Handler[strayPathReq, specout.NoContent]{HandlerFunc: okBody})
	wantBuildErr(t, d, `path:"Id"`)
}

// The same tag on the matching pattern builds, and a path:"-" opt-out is not
// a stray.
func TestPathFieldMatchesPattern(t *testing.T) {
	type req struct {
		ID   string `json:"-"    path:"id"`
		Skip string `json:"skip" path:"-"`
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x/{id}", specout.Handler[req, specout.NoContent]{HandlerFunc: okBody})
	require.Contains(t, docPaths(t, d), "/x/{id}", "path-typed route missing")
}
