package hijack_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/specout/internal/hijack"
	"github.com/stretchr/testify/assert"
)

func TestStatus(t *testing.T) {
	upgrade := http.Header{"Connection": {"keep-alive, Upgrade"}, "Upgrade": {"websocket"}}
	for _, tc := range []struct {
		name            string
		h               http.Header
		status          int
		wrote, hijacked bool
		want            int
		ok              bool
	}{
		{"not hijacked", nil, http.StatusOK, false, false, http.StatusOK, true},
		{"status before hijack", nil, http.StatusSwitchingProtocols, true, true, http.StatusSwitchingProtocols, true},
		{"handshake", upgrade, http.StatusOK, false, true, http.StatusSwitchingProtocols, true},
		{"token case", http.Header{"Connection": {"UPGRADE"}, "Upgrade": {"h2c"}}, 0, false, true, http.StatusSwitchingProtocols, true},
		{"no upgrade header", http.Header{"Connection": {"upgrade"}}, http.StatusOK, false, true, 0, false},
		{"no upgrade token", http.Header{"Connection": {"keep-alive"}, "Upgrade": {"websocket"}}, 0, false, true, 0, false},
		{"other hijack", nil, http.StatusOK, false, true, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, ok := hijack.Status(tc.h, tc.status, tc.wrote, tc.hijacked)
			assert.Equal(t, tc.want, status)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

// Wrap keeps the interfaces that chi's WrapResponseWriter and handlers test
// for, and leaves a writer that cannot hijack as it is.
func TestWrapKeepsInterfaces(t *testing.T) {
	w := httptest.NewRecorder()
	got, watch := hijack.Wrap(w)
	assert.Same(t, w, got)
	assert.False(t, watch.Hijacked())

	var full struct {
		http.ResponseWriter
		http.Flusher
		http.Hijacker
		io.ReaderFrom
	}
	got, watch = hijack.Wrap(full)
	assert.False(t, watch.Hijacked())
	assert.Implements(t, (*http.Flusher)(nil), got)
	assert.Implements(t, (*http.Hijacker)(nil), got)
	assert.Implements(t, (*io.ReaderFrom)(nil), got)
	assert.Implements(t, (*interface{ Unwrap() http.ResponseWriter })(nil), got)
}
