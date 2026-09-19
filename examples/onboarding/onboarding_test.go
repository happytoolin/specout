package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministicSpec(t *testing.T) {
	var first, second bytes.Buffer
	d1, _ := New()
	d2, _ := New()
	require.NoError(t, d1.WriteJSON(&first))
	require.NoError(t, d2.WriteJSON(&second))
	assert.Equal(t, first.String(), second.String())
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	_, api := New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/onboarding/onb_4f9x", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
