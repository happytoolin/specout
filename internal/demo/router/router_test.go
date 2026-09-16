package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The published spec declares security for the protected routes, so the app
// must actually enforce it. A binder captured on the root and used inside a
// group registers outside that group's middleware, silently dropping auth.
func TestProtectedRoutesRequireAuth(t *testing.T) {
	_, h := New()
	do := func(req *http.Request) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do(httptest.NewRequest(http.MethodPost, "/onboarding", nil)); got != http.StatusUnauthorized {
		t.Errorf("POST /onboarding without credentials = %d, want 401", got)
	}

	req := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader(`{"owner":"a@b.com","stage":"draft"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer demo-token")
	if got := do(req); got == http.StatusUnauthorized {
		t.Errorf("POST /onboarding with credentials = 401, want past the middleware")
	}

	if got := do(httptest.NewRequest(http.MethodGet, "/onboarding", nil)); got != http.StatusOK {
		t.Errorf("GET /onboarding = %d, want 200 (public)", got)
	}
}
