package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	gmux "github.com/gorilla/mux"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
)

// A handler returning a global-default code the spec declares (404) is not
// drift: DeclaredStatuses is route-specific, SpecStatuses adds the
// DefaultErrors envelope, and Verify must use the latter for this check.
func TestDeclaredDefaultNotDrift(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1",
		ErrorType: struct {
			Msg string `json:"msg"`
		}{},
		DefaultErrors: []int{400, 404, 500},
	})
	rec := chiRoute(t, d, "GET", "/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	serve(rec, "GET", "/x")
	wantNoErr(t, d, rec, "spec does not declare it", "declared default flagged as drift")
}

// HEAD against a GET-only route counts as the GET (finding 11): chi 405s the
// probe, and the recorder must not key that as drift against the GET.
func TestHeadMatchesGet(t *testing.T) {
	d := gen()
	rec := chiRoute(t, d, "GET", "/x", hit204)
	serve(rec, "GET", "/x")
	serve(rec, "HEAD", "/x")
	wantNoErr(t, d, rec, "", "HEAD drift")
}

// A route declared as HEAD stays HEAD: the recorder does not relabel it GET.
func TestDeclaredHeadNoDrift(t *testing.T) {
	d := gen()
	rec := chiRoute(t, d, "HEAD", "/x", hit204)
	serve(rec, "HEAD", "/x")
	wantNoErr(t, d, rec, "", "declared HEAD drift")
}

// Unmatched requests record nothing (finding 12): only the "declared never
// produced" half may fire, never a 404 drift.
func TestUnmatchedRecordsNothing(t *testing.T) {
	d := gen()
	rec := chiRoute(t, d, "GET", "/x", hit204)
	serve(rec, "GET", "/nope")
	wantNoErr(t, d, rec, "404", "404 keyed as drift")
}

// Unwrap lets http.ResponseController reach the real writer (finding 13).
func TestResponseControllerThroughRecorder(t *testing.T) {
	flushed := false
	r := chi.NewRouter()
	r.Get("/stream", func(w http.ResponseWriter, _ *http.Request) {
		flushed = http.NewResponseController(w).Flush() == nil
		_, _ = w.Write([]byte("hi"))
	})
	recorder.New(r).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/stream", nil))
	assert.True(t, flushed, "ResponseController could not reach the Flusher")
}

// gorilla carries the matched route on a request copy, so the recorder has
// to match itself: a plain and a regex template both key without drift.
func TestGorillaPatternCaptured(t *testing.T) {
	d := gen()
	g := gmux.NewRouter()
	gc := specout.Gorilla(d, g)
	gc.Get("/a/{id}", nc(hit204))
	gc.Get("/b/{id:[0-9]+}", nc(hit204))
	rec := recorder.New(g)
	serve(rec, "GET", "/a/7")
	serve(rec, "GET", "/b/7")
	wantNoErr(t, d, rec, "", "gorilla patterns not captured")
}

// A brace regex quantifier nests braces: the recorder must key it the same
// way the spec does, or a served request drifts against a path that exists.
func TestGorillaBraceQuantifierNoDrift(t *testing.T) {
	d := gen()
	g := gmux.NewRouter()
	specout.Gorilla(d, g).Get("/x/{id:[0-9]{4}}", nc(hit204))
	rec := recorder.New(g)
	serve(rec, "GET", "/x/2024")
	wantNoErr(t, d, rec, "", "brace quantifier drifted")
}

// The spec endpoint lives on the app router but is not an operation: a skip
// keeps it out of the drift check.
func TestSpecEndpointSkipped(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r, specout.Skip("/openapi.json"))
	serve(rec, "GET", "/openapi.json")
	wantNoErr(t, d, rec, "openapi.json", "spec endpoint flagged as drift")
}

// An outer http.ServeMux subtree mount ("/api/v3/") is not an operation. A
// request the app router does not match (404, or chi's 405 on a HEAD probe)
// carries that mount as r.Pattern; keying it would report drift against a
// path that is not a route.
func TestSubtreeMountNotKeyed(t *testing.T) {
	d := gen()
	rec := chiRoute(t, d, "GET", "/x", hit204)
	outer := http.NewServeMux()
	outer.Handle("/api/v3/", http.StripPrefix("/api/v3", rec))
	for _, c := range []struct{ method, target string }{
		{"GET", "/api/v3/nope"},
		{"HEAD", "/api/v3/x"},
	} {
		outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.target, nil))
	}
	wantNoErr(t, d, rec, "api/v3", "mount keyed as drift")
}

// Std ServeMux apps: r.Pattern is set during ServeHTTP; the recorder must
// read it after dispatch, not before.
func TestStdMuxPatternCaptured(t *testing.T) {
	mux := http.NewServeMux()
	d := gen()
	specout.Std(d, mux).Handle("GET /items/{id}", nc(hit204))
	rec := recorder.New(mux)
	serve(rec, "GET", "/items/9")
	wantNoErr(t, d, rec, "", "verify errors")
}
