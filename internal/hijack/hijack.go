// Package hijack notes when a handler takes over the connection, so that a
// status recorder can report the status the client got. After a Hijack the
// handler writes the response itself, as a WebSocket library writes its
// "101 Switching Protocols", and the response writer never sees that status.
package hijack

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Watch notes a successful Hijack through the writer that Wrap returned.
type Watch struct{ hijacked bool }

// Hijacked reports whether a handler hijacked the connection. A nil Watch
// saw no Hijack.
func (w *Watch) Hijacked() bool { return w != nil && w.hijacked }

// Hijack takes over the connection through hj, and notes the Hijack when it
// works.
func (w *Watch) Hijack(hj http.Hijacker) (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("hijack: %w", err)
	}
	w.hijacked = true
	return conn, rw, nil
}

// Wrap returns w wrapped so that a Hijack sets the returned Watch. The
// wrapper keeps the http.Flusher and io.ReaderFrom of w, which handlers and
// chi's middleware.WrapResponseWriter test for, and Unwrap, which
// http.ResponseController follows. A w that cannot hijack, such as an HTTP/2
// writer, is returned as it is, with a nil Watch.
func Wrap(w http.ResponseWriter) (http.ResponseWriter, *Watch) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return w, nil
	}
	watch := &Watch{}
	base := &writer{ResponseWriter: w, hj: hj, watch: watch}
	fl, flushes := w.(http.Flusher)
	rf, readsFrom := w.(io.ReaderFrom)
	switch {
	case flushes && readsFrom:
		return fancyWriter{flushWriter{base, fl}, rf}, watch
	case flushes:
		return flushWriter{base, fl}, watch
	default:
		return base, watch
	}
}

// Status returns the status to record for a response, and false when there
// is none to record. status is the code the writer saw, and wrote tells
// whether the handler wrote one before a Hijack. A hijacked request whose
// header h asks to switch protocols, and that wrote no status, got a 101
// from the handler itself. Any other hijack without a status leaves nothing
// to record. h is read only for such a hijack.
func Status(h http.Header, status int, wrote, hijacked bool) (int, bool) {
	switch {
	case !hijacked || wrote:
		return status, true
	case asksUpgrade(h):
		return http.StatusSwitchingProtocols, true
	default:
		return 0, false
	}
}

// asksUpgrade reports a request that asks to switch protocols: an Upgrade
// header and the upgrade token in Connection.
func asksUpgrade(h http.Header) bool {
	if h.Get("Upgrade") == "" {
		return false
	}
	for _, v := range h.Values("Connection") {
		for token := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

// writer is a hijacking http.ResponseWriter that notes a Hijack.
type writer struct {
	http.ResponseWriter

	hj    http.Hijacker
	watch *Watch
}

func (w *writer) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.watch.Hijack(w.hj) }

// Unwrap returns the wrapped writer, for http.ResponseController.
func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// flushWriter adds Flush for a writer that flushes.
type flushWriter struct {
	*writer
	http.Flusher
}

// fancyWriter adds ReadFrom for a writer that also reads from a reader, as
// net/http's HTTP/1 writer does.
type fancyWriter struct {
	flushWriter
	io.ReaderFrom
}
