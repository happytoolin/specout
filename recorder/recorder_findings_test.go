package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

func hit204(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }

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
