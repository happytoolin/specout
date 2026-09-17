package recorder_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/specout/internal/demo/router"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/require"
)

// TestVerifyPassesOnFullCoverage exercises every declared route and status
// once, then expects a clean Verify — the "recorder in your test suite"
// flow from the api-reference.
func TestVerifyPassesOnFullCoverage(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r)

	// one request per route and branch; the comment names what it produces
	for _, c := range []struct{ method, target, body string }{
		{http.MethodGet, "/onboarding", ""},                                                          // list 200
		{http.MethodPost, "/onboarding", `{"owner":"new@example.com","stage":"draft"}`},              // create 201
		{http.MethodPost, "/onboarding?existing=1", `{"owner":"again@example.com","stage":"draft"}`}, // idempotent 200
		{http.MethodGet, "/onboarding/onb_4f9x", ""},                                                 // get 200
		{http.MethodPut, "/onboarding/onb_4f9x", `{"owner":"up@example.com","stage":"active"}`},      // update 200
		{http.MethodPut, "/onboarding/onb_new1", `{"owner":"x@example.com","stage":"draft"}`},        // create 201
		{http.MethodPut, "/onboarding/onb_4f9x", `{"owner":"not-an-email","stage":"draft"}`},         // 422
		{http.MethodDelete, "/onboarding/onb_new1", ""},                                              // 204
		{http.MethodPost, "/onboarding/onb_4f9x/sync", `{"expected":0}`},                             // 200 (version 0)
		{http.MethodPost, "/onboarding/onb_4f9x/sync", `{"expected":999}`},                           // 409
		{http.MethodPost, "/onboarding/onb_4f9x/sync", `{"expected":-1}`},                            // 422 (negative)
		{http.MethodPost, "/files/import", ""},                                                       // 204
		{http.MethodGet, "/files/report", ""},                                                        // 200 binary
		{http.MethodPost, "/webhooks", `{"kind":"email","data":{"address":"ops@example.com"}}`},      // 200
		{http.MethodGet, "/legacy", ""},                                                              // 200
		{http.MethodGet, "/things", ""},                                                              // 200 search
		{http.MethodPost, "/things", `{"slug":"acme-thing","displayName":"Acme","email":"ops@example.com","priority":3,"stock":10,"tags":["core"],"visibility":"public"}`}, // 201
		{http.MethodPost, "/things", `{"slug":"AB"}`},                                                  // 422 (pattern fail)
		{http.MethodPost, "/channels", `{"kind":"notify_email","data":{"address":"ops@example.com"}}`}, // 204
	} {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer demo-token")
		rec.ServeHTTP(httptest.NewRecorder(), req)
	}
	wantNoErr(t, d, rec, "", "expected clean verify")
}

// TestVerifyFailsOnDeclaredButUnproduced: a fresh recorder hitting only one
// branch leaves the other declared codes unexercised — Verify must fire.
func TestVerifyFailsOnDeclaredButUnproduced(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r)

	req := httptest.NewRequest(http.MethodPost, "/onboarding/onb_4f9x/sync", strings.NewReader(`{"expected":5}`))
	req.Header.Set("Content-Type", "application/json")
	rec.ServeHTTP(httptest.NewRecorder(), req)

	require.NotEmpty(t, findErr(verify(d, rec), "never produced"), "expected never-produced failure")
}
