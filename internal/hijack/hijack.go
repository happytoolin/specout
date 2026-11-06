// Package hijack notes when a handler takes over the connection, so that a
// status recorder can report the status the client got. After a Hijack the
// handler writes the response itself, as a WebSocket library writes its
// "101 Switching Protocols", and the response writer never sees that status.
package hijack

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Watch notes the status that a handler writes through the writer that Wrap
// returned, and a successful Hijack.
type Watch struct {
	status   int
	hijacked bool
}

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

// Result returns the status to record for the request with header h, as
// Status does.
func (w *Watch) Result(h http.Header) (int, bool) {
	return Status(h, w.status, w.status != 0, w.hijacked)
}

// wrote notes code as the status, unless a status came first. An
// informational code other than 101 is no final status.
func (w *Watch) wrote(code int) {
	if w.status == 0 && (code >= http.StatusOK || code == http.StatusSwitchingProtocols) {
		w.status = code
	}
}

// Wrap returns w wrapped so that the returned Watch sees the status and a
// Hijack. The wrapper keeps the http.Flusher and http.Hijacker of w, which
// handlers test for, and Unwrap, which http.ResponseController follows.
func Wrap(w http.ResponseWriter) (http.ResponseWriter, *Watch) {
	watch := &Watch{}
	base := &writer{ResponseWriter: w, watch: watch}
	hj, hijacks := w.(http.Hijacker)
	fl, flushes := w.(http.Flusher)
	switch {
	case hijacks && flushes:
		return hijackFlushWriter{hijackWriter{base, hj}, fl}, watch
	case hijacks:
		return hijackWriter{base, hj}, watch
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

type writer struct {
	http.ResponseWriter

	watch *Watch
}

func (w *writer) WriteHeader(code int) {
	w.watch.wrote(code)
	w.ResponseWriter.WriteHeader(code)
}

func (w *writer) Write(b []byte) (int, error) {
	w.watch.wrote(http.StatusOK)
	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write: %w", err)
	}
	return n, nil
}

// Unwrap returns the wrapped writer, for http.ResponseController.
func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type hijackWriter struct {
	*writer

	hj http.Hijacker
}

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.watch.Hijack(w.hj) }

type flushWriter struct {
	*writer

	fl http.Flusher
}

func (w flushWriter) Flush() {
	w.watch.wrote(http.StatusOK)
	w.fl.Flush()
}

type hijackFlushWriter struct {
	hijackWriter

	fl http.Flusher
}

func (w hijackFlushWriter) Flush() {
	w.watch.wrote(http.StatusOK)
	w.fl.Flush()
}
