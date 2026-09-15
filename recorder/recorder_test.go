package recorder_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/happytoolin/specout/recorder"
)

type failT struct{ errs []string }

func (f *failT) Helper() {}
func (f *failT) Fatalf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}
func (f *failT) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

// TestVerifyPassesOnFullCoverage exercises every declared route and status
// once, then expects a clean Verify — the "recorder in your test suite"
// flow from the api-reference.
func TestVerifyPassesOnFullCoverage(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r)

	hit := func(method, target, body string) {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec.ServeHTTP(httptest.NewRecorder(), req)
	}

	hit(http.MethodGet, "/onboarding", "")                                                          // list 200
	hit(http.MethodPost, "/onboarding", `{"owner":"new@example.com","stage":"draft"}`)              // create 201
	hit(http.MethodPost, "/onboarding?existing=1", `{"owner":"again@example.com","stage":"draft"}`) // idempotent 200
	hit(http.MethodGet, "/onboarding/onb_4f9x", "")                                                 // get 200
	hit(http.MethodPut, "/onboarding/onb_4f9x", `{"owner":"up@example.com","stage":"active"}`)      // update 200
	hit(http.MethodPut, "/onboarding/onb_new1", `{"owner":"x@example.com","stage":"draft"}`)        // create 201
	hit(http.MethodPut, "/onboarding/onb_4f9x", `{"owner":"not-an-email","stage":"draft"}`)         // 422
	hit(http.MethodDelete, "/onboarding/onb_new1", "")                                              // 204
	hit(http.MethodPost, "/onboarding/onb_4f9x/sync", `{"expected":0}`)                             // 200 (version 0)
	hit(http.MethodPost, "/onboarding/onb_4f9x/sync", `{"expected":999}`)                           // 409
	hit(http.MethodPost, "/onboarding/onb_4f9x/sync", `{"expected":-1}`)                            // 422 (negative)
	hit(http.MethodPost, "/files/import", "")                                                       // 204
	hit(http.MethodGet, "/files/report", "")                                                        // 200 binary
	hit(http.MethodPost, "/webhooks", `{"kind":"email","data":{"address":"ops@example.com"}}`)      // 200
	hit(http.MethodGet, "/legacy", "")                                                              // 200

	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("expected clean verify, got: %v", ft.errs)
	}
}

// TestVerifyFailsOnDeclaredButUnproduced: a fresh recorder hitting only one
// branch leaves the other declared codes unexercised — Verify must fire.
func TestVerifyFailsOnDeclaredButUnproduced(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r)

	req := httptest.NewRequest(http.MethodPost, "/onboarding/onb_4f9x/sync", strings.NewReader(`{"expected":5}`))
	req.Header.Set("Content-Type", "application/json")
	rec.ServeHTTP(httptest.NewRecorder(), req)

	ft := &failT{}
	recorder.Verify(ft, d, rec)
	found := false
	for _, e := range ft.errs {
		if strings.Contains(e, "never produced") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected never-produced failure, got %v", ft.errs)
	}
}
