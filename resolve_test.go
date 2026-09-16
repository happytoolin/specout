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

// Bad method tokens panic (std Handle and Document).
func TestBadMethodPanics(t *testing.T) {
	mux := http.NewServeMux()
	cases := []func(){
		func() {
			specout.Std(d2(), mux).Handle("get /x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: okBody})
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
