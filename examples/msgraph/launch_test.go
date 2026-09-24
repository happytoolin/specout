package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLaunchWeirdBodyIsRejected(t *testing.T) {
	for _, body := range []string{`{"displayName":"changed"}{}`, `{"displayName":"changed"}null`, `{"displayName":"changed"} garbage`, ""} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/users", strings.NewReader(body))
		w := httptest.NewRecorder()
		_, ok := decode[User](w, r)
		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}
