package specout_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	gmux "github.com/gorilla/mux"
	"github.com/happytoolin/specout"
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
	if _, ok := declared[specout.RouteKey{Method: "GET", Path: "/late"}]; !ok {
		t.Fatal("late route unresolved before any build")
	}
	if _, ok := docPaths(t, d)["/late"]; !ok {
		t.Error("late route missing")
	}
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
	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Fatalf("pairing unstable: %v vs %v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("want 2 paths, have %v", first)
	}
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
	if _, ok := docPaths(t, d)["/files/*"]; ok {
		t.Error("catch-all leaked into paths")
	}
	if _, ok := declaredStatuses(t, d)[specout.RouteKey{Method: "GET", Path: "/files/*"}]; !ok {
		t.Error("catch-all missing from drift keys")
	}
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
	if decl[key][404] {
		t.Errorf("DeclaredStatuses carries the global envelope: %v", decl[key])
	}
	if !decl[key][204] {
		t.Errorf("DeclaredStatuses missing the route's own 204: %v", decl[key])
	}
	spec := specStatuses(t, d)
	for _, c := range []int{204, 400, 404, 500} {
		if !spec[key][c] {
			t.Errorf("SpecStatuses missing %d: %v", c, spec[key])
		}
	}

	// Omit removes a global default from one route only (both views).
	rc.Get("/y", noBody{HandlerFunc: okBody, Responses: []specout.Response{{Status: 404, Omit: true}}})
	key = specout.RouteKey{Method: "GET", Path: "/y"}
	decl, spec = declaredStatuses(t, d), specStatuses(t, d)
	if decl[key][404] || spec[key][404] {
		t.Errorf("Omit did not remove 404: decl=%v spec=%v", decl[key], spec[key])
	}
	if !spec[key][500] {
		t.Errorf("sibling default 500 dropped: %v", spec[key])
	}
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
	if paths := docPaths(t, d); paths["/items/{id}"] == nil {
		t.Errorf("/items/{id} missing; have %v", paths)
	}
	// live serve check
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "/items/9", nil))
	if w.Code != 204 {
		t.Fatalf("gorilla serve: %d", w.Code)
	}
}

// Gorilla Adopt: a plain gorilla route is a stray; a skip suppresses it.
func TestGorillaAdoptStrayAndSkip(t *testing.T) {
	d, g := newGen(), gmux.NewRouter()
	specout.Gorilla(d, g).Get("/ok", okGet)
	g.HandleFunc("/stray", strayHandler).Methods("GET")
	wantErr(t, specout.Gorilla(d, g).Adopt(), "/stray")
	if err := specout.Gorilla(d, g).Adopt(specout.Skip("/stray")); err != nil {
		t.Fatalf("skip should suppress: %v", err)
	}
}

// Gorilla Adopt: method-less route fails loud instead of silently skipping.
func TestGorillaMethodlessFails(t *testing.T) {
	g := gmux.NewRouter()
	g.HandleFunc("/x", okBody)
	wantErr(t, specout.Gorilla(newGen(), g).Adopt(), "no method constraint")
}

// Document escape hatch records absolute patterns for foreign routers.
func TestDocument(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/anything", okGet)
	if _, ok := docPaths(t, d)["/anything"]; !ok {
		t.Fatal("documented route missing")
	}
}

// Document is the whole integration for a router with no adapter: it records
// an absolute pattern with no Adopt, and the foreign mux still serves it.
func TestDocumentForeignRouterEndToEnd(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	specout.Document(d, http.MethodGet, "/echo/items/{id}", okGet)
	mux.HandleFunc("GET /echo/items/{id}", okGet.HandlerFunc)
	if _, ok := docPaths(t, d)["/echo/items/{id}"]; !ok {
		t.Fatal("Document path missing from spec")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/echo/items/7", nil))
	if rec.Code != 204 {
		t.Errorf("foreign mux served %d, want 204", rec.Code)
	}
}

// Bad method tokens and relative std patterns panic (std Handle and Document).
func TestBadMethodPanics(t *testing.T) {
	mux := http.NewServeMux()
	for _, bad := range []string{"get /x", "GET  /x", "GET x"} {
		panics(t, func() { specout.Std(newGen(), mux).Handle(bad, okGet) })
	}
	panics(t, func() { specout.Document(newGen(), "get", "/x", okGet) })
	panics(t, func() { specout.Document(newGen(), "GET", "x", okGet) })
}

// Gorilla composes subrouter prefixes into the route template, so the spec
// path is the one the server actually serves.
func TestGorillaSubrouterPrefixComposed(t *testing.T) {
	d, root := newGen(), gmux.NewRouter()
	specout.Gorilla(d, root.PathPrefix("/api").Subrouter()).Get("/items", okGet)
	if _, ok := docPaths(t, d)["/api/items"]; !ok {
		t.Fatalf("prefix lost; have %v", docPaths(t, d))
	}
	w := httptest.NewRecorder()
	root.ServeHTTP(w, httptest.NewRequest("GET", "/api/items", nil))
	if w.Code != 204 {
		t.Fatalf("serve: %d", w.Code)
	}
}

// A std multi-segment wildcard is a catch-all: kept for drift, out of paths.
func TestStdMultiSegmentWildcardOmitted(t *testing.T) {
	d := newGen()
	specout.Std(d, http.NewServeMux()).Handle("GET /files/{path...}", okGet)
	if _, ok := docPaths(t, d)["/files/{path...}"]; ok {
		t.Error("std {path...} leaked into paths")
	}
	if _, ok := declaredStatuses(t, d)[specout.RouteKey{Method: "GET", Path: "/files/{path...}"}]; !ok {
		t.Error("catch-all missing from drift keys")
	}
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
		if _, ok := paths[p]; !ok {
			t.Errorf("%s missing; have %v", p, paths)
		}
	}
}

// Adopt reports strays without breaking resolution: the walk source is
// registered even when the router holds undocumented routes.
func TestAdoptStraysStillResolve(t *testing.T) {
	d, r := newGen(), chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/ok", okGet)
	r.Get("/stray", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	wantErr(t, rc.Adopt(), "/stray")
	if _, ok := docPaths(t, d)["/ok"]; !ok {
		t.Fatalf("documented route lost to the stray: %v", docPaths(t, d))
	}
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
		if got := opOf(t, buildDoc(t, d), path, "get")["summary"]; got != summary {
			t.Errorf("%s summary = %q, want %q", path, got, summary)
		}
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
	if !ok {
		t.Fatalf("regex constraint leaked into the path: %v", paths)
	}
	if got := op["get"].(map[string]any)["operationId"]; got != "getItemsId" {
		t.Errorf("operationId = %q, want getItemsId", got)
	}
}

// A gorilla PathPrefix Subrouter parent has no handler and is a mount, not
// an endpoint: Adopt must not flag it.
func TestGorillaSubrouterAdoptClean(t *testing.T) {
	d, root := newGen(), gmux.NewRouter()
	specout.Gorilla(d, root.PathPrefix("/api").Subrouter()).Get("/items", okGet)
	if err := specout.Gorilla(d, root).Adopt(); err != nil {
		t.Fatalf("subrouter parent flagged as stray: %v", err)
	}
}

// A brace regex quantifier ({4}) nests braces inside the constraint. It must
// not leak a stray brace into the documented path or the operationId.
func TestBraceQuantifierParamNormalized(t *testing.T) {
	const pat = "/x/{id:[0-9]{4}}"
	check := func(name string, d *specout.Generator) {
		t.Helper()
		op, ok := docPaths(t, d)["/x/{id}"].(map[string]any)
		if !ok {
			t.Errorf("%s: want /x/{id}", name)
			return
		}
		if got := op["get"].(map[string]any)["operationId"]; got != "getXId" {
			t.Errorf("%s: operationId = %q, want getXId", name, got)
		}
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
	if err := specout.Gorilla(d, g).Adopt(); err != nil {
		t.Fatalf("spec mount flagged as stray: %v", err)
	}
}

// A zero Handler has no func: the binder must refuse it at registration
// instead of documenting an endpoint that panics when served.
func TestNilHandlerPanics(t *testing.T) {
	panics(t, func() {
		specout.Gorilla(newGen(), gmux.NewRouter()).Get("/x",
			noBody{Summary: "documents a segfault"})
	})
}

// A method OpenAPI cannot name (CONNECT) would emit an invalid path-item
// key, so the binder panics instead of writing an invalid document.
func TestNonOpenAPIMethodPanics(t *testing.T) {
	panics(t, func() { specout.Document(newGen(), "CONNECT", "/x", okGet) })
	panics(t, func() { specout.Std(newGen(), http.NewServeMux()).Handle("CONNECT /x", okGet) })
}

// A repeated path param name (/x/{id}/y/{id}) is one OpenAPI parameter:
// OpenAPI forbids two entry names in one operation.
func TestDuplicatePathParamEmittedOnce(t *testing.T) {
	d := newGen()
	specout.Document(d, http.MethodGet, "/x/{id}/y/{id}", okGet)
	params, _ := opOf(t, buildDoc(t, d), "/x/{id}/y/{id}", "get")["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("want 1 path param, got %d: %v", len(params), params)
	}
	if got := params[0].(map[string]any)["name"]; got != "id" {
		t.Errorf("param name = %v, want id", got)
	}
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
		ID   string `path:"id" json:"-"`
		Skip string `path:"-" json:"skip"`
	}
	d := newGen()
	specout.Document(d, http.MethodGet, "/x/{id}", specout.Handler[req, specout.NoContent]{HandlerFunc: okBody})
	if _, ok := docPaths(t, d)["/x/{id}"]; !ok {
		t.Fatal("path-typed route missing")
	}
}
