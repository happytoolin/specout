package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/happytoolin/specout/internal/demo/router"
)

// The spec declares security for the protected routes, so the app must enforce
// it: a root-captured binder registers outside a group's middleware, dropping auth.
func TestProtectedRoutesRequireAuth(t *testing.T) {
	_, h := router.New()
	do := func(req *http.Request) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	noCreds := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/onboarding", nil)
	assert.Equal(t, http.StatusUnauthorized, do(noCreds), "POST /onboarding without credentials")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/onboarding", strings.NewReader(`{"owner":"a@b.com","stage":"draft"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer demo-token")
	assert.NotEqual(t, http.StatusUnauthorized, do(req), "POST /onboarding with credentials")

	list := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/onboarding", nil)
	assert.Equal(t, http.StatusOK, do(list), "GET /onboarding (public)")
}
