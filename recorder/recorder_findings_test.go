package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	gmux "github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/happytoolin/specout/recorder"
)

func hit204(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }

type errBody struct {
	Msg string `json:"msg"`
}

// A handler returning a global-default code the spec declares (404) is not
// drift. Regression: DeclaredStatuses is route-specific, SpecStatuses adds
// the DefaultErrors envelope, and Verify must use the latter for this check.
func TestDeclaredDefaultNotDrift(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1",
		ErrorType: errBody{}, DefaultErrors: []int{400, 404, 500},
	})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/x", specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) },
	})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	rec := recorder.New(r)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	for _, e := range ft.errs {
		if strings.Contains(e, "spec does not declare it") {
			t.Errorf("declared default flagged as drift: %s", e)
		}
	}
}

// HEAD against a GET-only route counts as the GET (finding 11).
func TestHeadMatchesGet(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	rec := recorder.New(r)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	// HEAD probe on a GET-only route: chi 405s it; the recorder must not
	// key it as drift against the declared GET.
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("HEAD", "/x", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("HEAD drift: %s", strings.Join(ft.errs, "; "))
	}
}

// Unmatched requests record nothing (finding 12).
// A route declared as HEAD stays HEAD: the recorder does not relabel it GET.
func TestDeclaredHeadNoDrift(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Head("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	rec := recorder.New(r)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("HEAD", "/x", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("declared HEAD drift: %s", strings.Join(ft.errs, "; "))
	}
}

func TestUnmatchedRecordsNothing(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	rec := recorder.New(r)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/nope", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	// only the "declared never produced" half may fire; the 404 must not
	for _, e := range ft.errs {
		if strings.Contains(e, "404") {
			t.Errorf("404 keyed as drift: %s", e)
		}
	}
}

// Unwrap lets http.ResponseController reach the real writer (finding 13).
func TestResponseControllerThroughRecorder(t *testing.T) {
	var flushed bool
	done := make(chan struct{})
	r := chi.NewRouter()
	r.Get("/stream", func(w http.ResponseWriter, req *http.Request) {
		rc := http.NewResponseController(w)
		flushed = rc.Flush() == nil
		close(done)
		_, _ = w.Write([]byte("hi"))
	})
	rec := recorder.New(r)
	w := httptest.NewRecorder()
	rec.ServeHTTP(w, httptest.NewRequest("GET", "/stream", nil))
	<-done
	if !flushed {
		t.Error("ResponseController could not reach the Flusher")
	}
}

// gorilla carries the matched route on a request copy, so the recorder has
// to match itself: a plain and a regex template both key without drift.
func TestGorillaPatternCaptured(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	gc := specout.Gorilla(d, g)
	gc.Get("/a/{id}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	gc.Get("/b/{id:[0-9]+}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	rec := recorder.New(g)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/a/7", nil))
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/b/7", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("gorilla patterns not captured: %s", strings.Join(ft.errs, "; "))
	}
}

// A brace regex quantifier nests braces: the recorder must key it the same
// way the spec does, or a served request drifts against a path that exists.
func TestGorillaBraceQuantifierNoDrift(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	g := gmux.NewRouter()
	specout.Gorilla(d, g).Get("/x/{id:[0-9]{4}}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	rec := recorder.New(g)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x/2024", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("brace quantifier drifted: %s", strings.Join(ft.errs, "; "))
	}
}

// The spec endpoint lives on the app router but is not an operation: a skip
// keeps it out of the drift check.
func TestSpecEndpointSkipped(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r, specout.Skip("/openapi.json"))
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/openapi.json", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	for _, e := range ft.errs {
		if strings.Contains(e, "openapi.json") {
			t.Fatalf("spec endpoint flagged as drift: %s", e)
		}
	}
}

// An outer http.ServeMux subtree mount ("/api/v3/") is not an operation. A
// request the app router does not match (404, or chi's 405 on a HEAD probe)
// carries that mount as r.Pattern; keying it would report drift against a
// path that is not a route.
func TestSubtreeMountNotKeyed(t *testing.T) {
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	r := chi.NewRouter()
	rc := specout.Chi(d, r)
	rc.Get("/x", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: hit204})
	if err := rc.Adopt(); err != nil {
		t.Fatal(err)
	}
	rec := recorder.New(r)
	outer := http.NewServeMux()
	outer.Handle("/api/v3/", http.StripPrefix("/api/v3", rec))
	outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v3/nope", nil))
	outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("HEAD", "/api/v3/x", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	for _, e := range ft.errs {
		if strings.Contains(e, "api/v3") {
			t.Errorf("mount keyed as drift: %s", e)
		}
	}
}
