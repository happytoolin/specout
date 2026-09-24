package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLaunchWeirdBodyCannotRunAction(t *testing.T) {
	for _, body := range []string{`{"name":"changed"}{}`, `{"name":"changed"}null`, `{"name":"changed"} garbage`, ""} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/pet", strings.NewReader(body))
		w := httptest.NewRecorder()
		called := false
		withBody(w, r, "invalid JSON", func(map[string]any) { called = true })
		assert.False(t, called, "invalid bodies must not reach the store action")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}
