package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoldenSpec(t *testing.T) {
	var got bytes.Buffer
	d, _ := New()
	require.NoError(t, d.WriteJSON(&got))
	want, err := os.ReadFile("openapi.json")
	require.NoError(t, err, "golden missing; run: just golden")
	assert.Equal(t, string(want), got.String())
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	_, api := New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/onboarding/onb_4f9x", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMain(m *testing.M) {
	if os.Getenv("UPDATE_GOLDEN") != "" {
		var spec bytes.Buffer
		d, _ := New()
		if err := d.WriteJSON(&spec); err != nil {
			panic(err)
		}
		if err := os.WriteFile("openapi.json", spec.Bytes(), 0o600); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
