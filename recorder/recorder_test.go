package recorder_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/specout/internal/demoapp"
	"github.com/happytoolin/specout/recorder"
)

type failT struct {
	errs []string
}

func (f *failT) Errorf(format string, args ...any) {
	f.errs = append(f.errs, sprintf(format, args...))
}

func (f *failT) Helper() {}

func (f *failT) Fatalf(format string, args ...any) {
	f.errs = append(f.errs, sprintf(format, args...))
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func TestVerifyPassesOnFullCoverage(t *testing.T) {
	d, r, _ := demoapp.New()
	rec := recorder.New(r)

	// exercise every declared route/status in one pass
	req := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPut, "/onboarding/onb_1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding/onb_1/sync", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding/onb_1/sync?conflict=1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding/onb_1/sync?case=conflict", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding/onb_1/sync?case=invalid", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPut, "/onboarding/onb_1?created=1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPut, "/onboarding/onb_1?invalid=1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPut, "/onboarding/onb_1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding?existing=1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodDelete, "/onboarding/onb_1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodGet, "/onboarding/onb_1", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/onboarding", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/files/import", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodGet, "/files/report", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodGet, "/legacy", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/webhooks", nil)
	rec.ServeHTTP(httptest.NewRecorder(), req)

	ft := &failT{}
	recorder.Verify(ft, d, rec)
	if len(ft.errs) > 0 {
		t.Fatalf("expected clean verify, got: %v", ft.errs)
	}
}

func TestVerifyFailsOnUndeclaredCode(t *testing.T) {
	d, r, _ := demoapp.New()
	// don't build d; DeclaredStatuses works without build
	rec := recorder.New(r)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/onboarding/onb_1?conflict=1", nil))
	ft := &failT{}
	recorder.Verify(ft, d, rec)
	found := false
	for _, e := range ft.errs {
		if contains(e, "409") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 409-undeclared failure, got %v", ft.errs)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
