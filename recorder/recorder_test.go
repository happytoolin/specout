package recorder_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVerifyPassesOnFullCoverage exercises every declared status once, then
// expects a clean Verify.
func TestVerifyPassesOnFullCoverage(t *testing.T) {
	d, rec := statusRoute(t)
	for _, target := range []string{"/items", "/items?status=201", "/items?status=409"} {
		serve(rec, http.MethodPost, target)
	}
	wantNoErr(t, d, rec, "", "expected clean verify")
}

// TestVerifyFailsOnDeclaredButUnproduced: a fresh recorder hitting only one
// branch leaves the other declared codes unexercised — Verify must fire.
func TestVerifyFailsOnDeclaredButUnproduced(t *testing.T) {
	d, rec := statusRoute(t)
	serve(rec, http.MethodPost, "/items")
	require.NotEmpty(t, findErr(verify(d, rec), "never produced"), "expected never-produced failure")
}
