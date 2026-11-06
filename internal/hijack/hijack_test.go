package hijack_test

import (
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

func TestWrapKeepsInterfaces(t *testing.T) {
	got, _ := hijack.Wrap(httptest.NewRecorder())
	assert.Implements(t, (*http.Flusher)(nil), got)
	assert.NotImplements(t, (*http.Hijacker)(nil), got)

	var full struct {
		http.ResponseWriter
		http.Flusher
		http.Hijacker
	}
	got, watch := hijack.Wrap(full)
	assert.False(t, watch.Hijacked())
	assert.Implements(t, (*http.Flusher)(nil), got)
	assert.Implements(t, (*http.Hijacker)(nil), got)
	assert.Implements(t, (*interface{ Unwrap() http.ResponseWriter })(nil), got)
}

func TestWrapSeesStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		write func(w http.ResponseWriter)
		want  int
	}{
		"nothing":       {func(http.ResponseWriter) {}, 0},
		"body":          {func(w http.ResponseWriter) { _, _ = w.Write([]byte("x")) }, http.StatusOK},
		"status":        {func(w http.ResponseWriter) { w.WriteHeader(http.StatusTeapot) }, http.StatusTeapot},
		"early hints":   {func(w http.ResponseWriter) { w.WriteHeader(http.StatusEarlyHints); w.WriteHeader(http.StatusCreated) }, http.StatusCreated},
		"flush":         {func(w http.ResponseWriter) { w.(http.Flusher).Flush() }, http.StatusOK},
		"second status": {func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted); w.WriteHeader(http.StatusOK) }, http.StatusAccepted},
	} {
		w, watch := hijack.Wrap(httptest.NewRecorder())
		tc.write(w)
		status, ok := watch.Result(nil)
		assert.True(t, ok, name)
		assert.Equal(t, tc.want, status, name)
	}
}
