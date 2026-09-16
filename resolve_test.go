package specout_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	gmux "github.com/gorilla/mux"
	"github.com/happytoolin/specout"
)

func okBody(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }

func docPaths(t *testing.T, d *specout.Generator) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatalf("build: %v", err)
	}
	var doc map[string]any
	json.Unmarshal(buf.Bytes(), &doc)
	return doc["paths"].(map[string]any)
}

// Registration after one build still resolves on the next build: the
// resolver is not a one-shot latch (freeze still applies after serve).
func TestLateRegistrationResolves(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/early", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	// no build yet; register a second route, then build once
	rc.Get("/late", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	declared, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := declared[specout.RouteKey{Method: "GET", Path: "/late"}]; !ok {
		t.Fatal("late route unresolved before any build")
	}
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if _, ok := docPaths(t, d)["/late"]; !ok {
		t.Error("late route missing")
	}
}

// Missing adopt: relative pattern has no composed full path -> build error.
func TestUnresolvedPatternFails(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		specout.Chi(d, r).Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	})
	// no Adopt of the root; the inner registration is relative
	var buf bytes.Buffer
	err := d.WriteJSON(&buf)
	if err == nil || !strings.Contains(err.Error(), "never resolved") {
		t.Fatalf("want unresolved error, got %v", err)
	}
}

// One func on two chi routes pairs deterministically across two builds.
func TestSharedHandlerPairingStable(t *testing.T) {
	build := func() []string {
		d := specout.New(specout.Config{Title: "t", Version: "1"})
		r := chi.NewRouter()
		rc := specout.Chi(d, r)
		rc.Get("/a/{id}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		rc.Get("/b/{id}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		if err := rc.Adopt(); err != nil {
			t.Fatal(err)
		}
		return sortedPaths(docPaths(t, d))
	}
	first := build()
	second := build()
	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Fatalf("pairing unstable: %v vs %v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("want 2 paths, have %v", first)
	}
}

func sortedPaths(paths map[string]any) []string {
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// chi /a and /a/ are distinct walk patterns; their derived operationIds
// collide, so the build demands an explicit OperationID on one of them.
func TestChiSlashVariantsDistinct(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/a", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	rc.Get("/a/", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	err := func() error {
		var buf bytes.Buffer
		if e := d.WriteJSON(&buf); e != nil {
			return e
		}
		return nil
	}()
	if err == nil || !strings.Contains(err.Error(), "operationId") {
		t.Fatalf("want operationId collision error, got %v", err)
	}
}

// Catch-alls are omitted from paths but stay in DeclaredStatuses.
func TestCatchAllOmittedButDeclared(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/files/*", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	if _, ok := docPaths(t, d)["/files/*"]; ok {
		t.Error("catch-all leaked into paths")
	}
	declared, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := declared[specout.RouteKey{Method: "GET", Path: "/files/*"}]; !ok {
		t.Errorf("catch-all missing from drift keys; have %v", declared)
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
	rc.Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	key := specout.RouteKey{Method: "GET", Path: "/x"}
	decl, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if decl[key][404] {
		t.Errorf("DeclaredStatuses carries the global envelope: %v", decl[key])
	}
	if !decl[key][204] {
		t.Errorf("DeclaredStatuses missing the route's own 204: %v", decl[key])
	}
	spec, err := d.SpecStatuses()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []int{204, 400, 404, 500} {
		if !spec[key][c] {
			t.Errorf("SpecStatuses missing %d: %v", c, spec[key])
		}
	}

	// Omit removes a global default from one route only (both views).
	rc.Get("/y", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: okBody,
		Responses:   []specout.Response{{Status: 404, Omit: true}},
	})
	key = specout.RouteKey{Method: "GET", Path: "/y"}
	decl, _ = d.DeclaredStatuses()
	spec, _ = d.SpecStatuses()
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	gc := specout.Gorilla(d, g)
	gc.Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	gc.Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }})
	var buf bytes.Buffer
	err := d.WriteJSON(&buf)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

// Gorilla: verbs, path params, template match.
func TestGorillaAdapter(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	gc := specout.Gorilla(d, g)
	gc.Get("/items/{id}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	gc.Post("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	paths := docPaths(t, d)
	if _, ok := paths["/items/{id}"]; !ok {
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	specout.Gorilla(d, g).Get("/ok", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	g.HandleFunc("/stray", strayHandler).Methods("GET")
	if err := specout.Gorilla(d, g).Adopt(); err == nil || !strings.Contains(err.Error(), "/stray") {
		t.Fatalf("want /stray stray, got %v", err)
	}
	if err := specout.Gorilla(d, g).Adopt(specout.Skip("/stray")); err != nil {
		t.Fatalf("skip should suppress: %v", err)
	}
}

// Gorilla Adopt: method-less route fails loud instead of silently skipping.
func TestGorillaMethodlessFails(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	g.HandleFunc("/x", okBody)
	err := specout.Gorilla(d, g).Adopt()
	if err == nil || !strings.Contains(err.Error(), "no method constraint") {
		t.Fatalf("want methodless error, got %v", err)
	}
}

// Document escape hatch records absolute patterns for foreign routers.
func TestDocument(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Document(d, http.MethodGet, "/anything", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if _, ok := docPaths(t, d)["/anything"]; !ok {
		t.Fatal("documented route missing")
	}
}

// Document is the whole integration for a router with no adapter: it records
// an absolute pattern with no Adopt, and the foreign mux still serves it.
func TestDocumentForeignRouterEndToEnd(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	mux := http.NewServeMux()
	h := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody}
	specout.Document(d, http.MethodGet, "/echo/items/{id}", h)
	mux.HandleFunc("GET /echo/items/{id}", h.HandlerFunc)
	if _, ok := docPaths(t, d)["/echo/items/{id}"]; !ok {
		t.Fatal("Document path missing from spec")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/echo/items/7", nil))
	if rec.Code != 204 {
		t.Errorf("foreign mux served %d, want 204", rec.Code)
	}
}

// Bad method tokens panic (std Handle and Document).
func TestBadMethodPanics(t *testing.T) {
	mux := http.NewServeMux()
	cases := []func(){
		func() {
			specout.Std(d2(), mux).Handle("get /x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
		func() {
			specout.Std(d2(), mux).Handle("GET  /x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
		func() {
			specout.Std(d2(), mux).Handle("GET x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
		func() {
			specout.Document(d2(), "get", "/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
		func() {
			specout.Document(d2(), "GET", "x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
	}
	for i, fn := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d: expected panic", i)
				}
			}()
			fn()
		}()
	}
}

func d2() *specout.Generator { return specout.New(specout.Config{Title: "t", Version: "1"}) }

// Gorilla composes subrouter prefixes into the route template, so the spec
// path is the one the server actually serves.
func TestGorillaSubrouterPrefixComposed(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	root := gmux.NewRouter()
	specout.Gorilla(d, root.PathPrefix("/api").Subrouter()).Get("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Std(d, http.NewServeMux()).Handle("GET /files/{path...}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if _, ok := docPaths(t, d)["/files/{path...}"]; ok {
		t.Error("std {path...} leaked into paths")
	}
	declared, err := d.DeclaredStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := declared[specout.RouteKey{Method: "GET", Path: "/files/{path...}"}]; !ok {
		t.Errorf("catch-all missing from drift keys; have %v", declared)
	}
}

// One func on a std mux and a chi router keeps both paths: the absolute
// record is not overwritten by the chi walk.
func TestStdAndChiShareFunc(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	h := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody}
	specout.Std(d, http.NewServeMux()).Handle("GET /std", h)
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/chi", h)
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/ok", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	r.Get("/stray", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	if err := rc.Adopt(); err == nil || !strings.Contains(err.Error(), "/stray") {
		t.Fatalf("want /stray stray, got %v", err)
	}
	if _, ok := docPaths(t, d)["/ok"]; !ok {
		t.Fatalf("documented route lost to the stray: %v", docPaths(t, d))
	}
}

// A shared handler keeps its own metadata: pairing never swaps /a and /b/c.
func TestSharedHandlerMetadataNotSwapped(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	h := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody, Summary: "SHORT"}
	rc.Get("/a", h)
	h.Summary = "LONG"
	rc.Get("/b/c", h)
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"/a": "SHORT", "/b/c": "LONG"}
	for path, summary := range want {
		item, _ := docPaths(t, d)[path].(map[string]any)
		op, _ := item["get"].(map[string]any)
		if got, _ := op["summary"].(string); got != summary {
			t.Errorf("%s summary = %q, want %q", path, got, summary)
		}
	}
}

// One route reachable at several paths it never registered is a build
// error naming every path: documenting one would silently drop the rest.
func TestDualMountFailsLoud(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	root, sub := chi.NewRouter(), chi.NewRouter()
	specout.Chi(d, sub).Get("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	root.Mount("/v1", sub)
	root.Mount("/v2", sub)
	if err := specout.Chi(d, root).Adopt(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := d.WriteJSON(&buf)
	if err == nil || !strings.Contains(err.Error(), "/v2/items") {
		t.Fatalf("want surplus-path error, got %v", err)
	}
}

// Adopting a subrouter and its parent registers two walk sources; the
// relative view is not a second mount point, so this fails loud too.
func TestSubAndRootAdoptedFailsLoud(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	root, sub := chi.NewRouter(), chi.NewRouter()
	specout.Chi(d, sub).Get("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	root.Mount("/api", sub)
	if err := specout.Chi(d, sub).Adopt(); err != nil {
		t.Fatal(err)
	}
	if err := specout.Chi(d, root).Adopt(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := d.WriteJSON(&buf)
	if err == nil || !strings.Contains(err.Error(), "outermost") {
		t.Fatalf("want outermost-adopt hint, got %v", err)
	}
}

// Router regex constraints are not OpenAPI templates: /items/{id:[0-9]+}
// documents as /items/{id}, and the derived operationId stays a Go name.
func TestRegexParamPathNormalized(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Gorilla(d, gmux.NewRouter()).Get("/items/{id:[0-9]+}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	paths := docPaths(t, d)
	if _, ok := paths["/items/{id}"]; !ok {
		t.Fatalf("regex constraint leaked into the path: %v", paths)
	}
	op := paths["/items/{id}"].(map[string]any)["get"].(map[string]any)
	if got, _ := op["operationId"].(string); got != "getItemsId" {
		t.Errorf("operationId = %q, want getItemsId", got)
	}
}

// A gorilla PathPrefix Subrouter parent has no handler and is a mount, not
// an endpoint: Adopt must not flag it.
func TestGorillaSubrouterAdoptClean(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	root := gmux.NewRouter()
	specout.Gorilla(d, root.PathPrefix("/api").Subrouter()).Get("/items", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	if err := specout.Gorilla(d, root).Adopt(); err != nil {
		t.Fatalf("subrouter parent flagged as stray: %v", err)
	}
}

// A brace regex quantifier ({4}) nests braces inside the constraint. It must
// not leak a stray brace into the documented path or the operationId.
func TestBraceQuantifierParamNormalized(t *testing.T) {
	const pat = "/x/{id:[0-9]{4}}"
	h := specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody}
	check := func(name string, d *specout.Generator) {
		t.Helper()
		paths := docPaths(t, d)
		op, ok := paths["/x/{id}"].(map[string]any)
		if !ok {
			t.Errorf("%s: want /x/{id}; have %v", name, paths)
			return
		}
		if got, _ := op["get"].(map[string]any)["operationId"].(string); got != "getXId" {
			t.Errorf("%s: operationId = %q, want getXId", name, got)
		}
	}
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Gorilla(d, gmux.NewRouter()).Get(pat, h)
	check("gorilla", d)
	d = specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	specout.Chi(d, r).Get(pat, h)
	if err := specout.Chi(d, r).Adopt(); err != nil {
		t.Fatal(err)
	}
	check("chi", d)
	d = specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Document(d, http.MethodGet, pat, h)
	check("document", d)
}

// Two routes with different regex but one documented path are a real
// collision: the second would silently overwrite the first.
func TestDocPathCollisionFails(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	gc := specout.Gorilla(d, gmux.NewRouter())
	gc.Get("/x/{id:[0-9]+}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody, OperationID: "alpha"})
	gc.Get("/x/{id:[a-z]+}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody, OperationID: "beta"})
	var buf bytes.Buffer
	err := d.WriteJSON(&buf)
	if err == nil || !strings.Contains(err.Error(), "/x/{id}") {
		t.Fatalf("want doc-path collision error, got %v", err)
	}
}

// Serving the spec from the same gorilla router it documents is a mount, not
// an endpoint: Adopt must stay quiet.
func TestGorillaSpecMountNotStray(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	specout.Gorilla(d, g).Get("/ok", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	g.Handle("/openapi.json", d)
	if err := specout.Gorilla(d, g).Adopt(); err != nil {
		t.Fatalf("spec mount flagged as stray: %v", err)
	}
}

// A zero Handler has no func: the binder must refuse it at registration
// instead of documenting an endpoint that panics when served.
func TestNilHandlerPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("want panic for a nil HandlerFunc")
		}
		if s, _ := r.(string); !strings.Contains(s, "nil HandlerFunc") {
			t.Errorf("panic = %v", r)
		}
	}()
	specout.Gorilla(d2(), gmux.NewRouter()).Get("/x", specout.Handler[struct{}, specout.NoContent]{
		Summary: "documents a segfault",
	})
}

// A method OpenAPI cannot name (CONNECT) would emit an invalid path-item
// key, so the binder panics instead of writing an invalid document.
func TestNonOpenAPIMethodPanics(t *testing.T) {
	cases := []func(){
		func() {
			specout.Document(d2(), "CONNECT", "/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
		func() {
			specout.Std(d2(), http.NewServeMux()).Handle("CONNECT /x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
		},
	}
	for i, fn := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d: expected panic", i)
				}
			}()
			fn()
		}()
	}
}

// A repeated path param name (/x/{id}/y/{id}) is one OpenAPI parameter:
// OpenAPI forbids two entry names in one operation.
func TestDuplicatePathParamEmittedOnce(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Document(d, http.MethodGet, "/x/{id}/y/{id}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
	op := docPaths(t, d)["/x/{id}/y/{id}"].(map[string]any)["get"].(map[string]any)
	params, _ := op["parameters"].([]any)
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
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Document(d, http.MethodGet, "/x/{id}", specout.Handler[strayPathReq, specout.NoContent]{HandlerFunc: okBody})
	var buf bytes.Buffer
	err := d.WriteJSON(&buf)
	if err == nil || !strings.Contains(err.Error(), `path:"Id"`) {
		t.Fatalf("want a stray path field error, got %v", err)
	}
}

// The same tag on the matching pattern builds, and a path:"-" opt-out is not
// a stray.
func TestPathFieldMatchesPattern(t *testing.T) {
	type req struct {
		ID   string `path:"id" json:"-"`
		Skip string `path:"-" json:"skip"`
	}
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	specout.Document(d, http.MethodGet, "/x/{id}", specout.Handler[req, specout.NoContent]{HandlerFunc: okBody})
	if _, err := docPathsErr(t, d); err != nil {
		t.Fatalf("build: %v", err)
	}
}

func docPathsErr(t *testing.T, d *specout.Generator) (map[string]any, error) {
	t.Helper()
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		return nil, err
	}
	var doc map[string]any
	json.Unmarshal(buf.Bytes(), &doc)
	return doc["paths"].(map[string]any), nil
}
