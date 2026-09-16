package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/recorder"
)

// Std ServeMux apps: r.Pattern is set during ServeHTTP; the recorder must
// read it after dispatch, not before.
func TestStdMuxPatternCaptured(t *testing.T) {
	mux := http.NewServeMux()
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
	d.Handle(mux, "GET /items/{id}", specout.Handler[struct{}, specout.NoContent]{HandlerFunc: ok})
	rec := recorder.New(mux)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/items/9", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("verify errors: %s", strings.Join(ft.errs, "; "))
	}
}
