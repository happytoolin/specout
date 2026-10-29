// Package adaptertest is the test plumbing the router adapter tests share. It
// is internal to this repo and is never part of the library.
package adaptertest

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit/exampletest"
)

// FakeT collects the failures a verification helper reports.
type FakeT = exampletest.CaptureT

// Generator returns a generator for a minimal document.
func Generator() *specout.Generator {
	return specout.New(specout.Config{Title: "t", Version: "1"})
}

// Paths builds d and returns its paths object.
func Paths(tb testing.TB, d *specout.Generator) map[string]any {
	tb.Helper()
	return exampletest.Obj(tb, exampletest.Spec(tb, d), "paths")
}

// PanicMessage runs f and returns what it panicked with. The test fails when
// f returns without a panic.
func PanicMessage(tb testing.TB, f func()) string {
	tb.Helper()
	var got any
	func() {
		defer func() { got = recover() }()
		f()
	}()
	if got == nil {
		tb.Fatal("no panic")
	}
	return fmt.Sprint(got)
}

// Strays returns the routes that the *specout.StrayError in err names. The
// test fails when err holds none.
func Strays(tb testing.TB, err error) []string {
	tb.Helper()
	if stray, ok := errors.AsType[*specout.StrayError](err); ok {
		return stray.Routes
	}
	tb.Fatalf("%v is not a *specout.StrayError", err)
	return nil
}

// HijackCase is one hijacked response for the recorder tests: what the
// handler does, whether the request asks to switch protocols, the status the
// client gets, and what recorder.Verify reports for the GET /ws route of
// Route. Flushed marks a handler that flushes a status before the hijack,
// which fasthttp's net/http adaptor cannot serve.
type HijackCase struct {
	Name    string
	Handler http.HandlerFunc
	Upgrade bool
	Flushed bool
	Status  int
	Errs    []string
}

// Route is the route the case runs on. It declares only a 101 response.
func (tc HijackCase) Route() specout.Handler[struct{}, specout.NoContent] {
	return specout.Handler[struct{}, specout.NoContent]{
		HandlerFunc: tc.Handler,
		Responses:   []specout.Response{{Status: http.StatusSwitchingProtocols}},
	}
}

// HijackCases are the hijacked responses that every recorder must read: a
// WebSocket handshake that writes its own 101 counts as 101, a status
// written before the hijack counts as itself, and another hijack counts as
// nothing, so its 101 stays unproduced.
func HijackCases() []HijackCase {
	return []HijackCase{
		{
			Name:    "handshake",
			Handler: Hijack(0, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test"),
			Upgrade: true, Status: http.StatusSwitchingProtocols,
		},
		{
			Name:    "status before hijack",
			Handler: Hijack(http.StatusSwitchingProtocols, ""),
			Upgrade: true, Flushed: true, Status: http.StatusSwitchingProtocols,
		},
		{
			Name:    "other hijack",
			Handler: Hijack(0, "HTTP/1.1 200 OK\r\nContent-Length: 0"),
			Status:  http.StatusOK, Errs: []string{"declared GET /ws 101 never produced by any test"},
		},
	}
}

// Hijack returns a handler that takes over the connection and writes the
// response head line itself, as gorilla/websocket does for its handshake, so
// the response writer never sees that status. A pre status other than 0 is
// written and flushed before the hijack, as other WebSocket libraries do.
func Hijack(pre int, line string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		rc := http.NewResponseController(w)
		if pre != 0 {
			w.WriteHeader(pre)
			_ = rc.Flush()
		}
		conn, brw, err := rc.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = conn.Close() }()
		if line != "" {
			_, _ = brw.WriteString(line + "\r\n\r\n")
			_ = brw.Flush()
		}
	}
}

// RawStatus sends one GET for target to h on a real HTTP/1.1 server and
// returns the status the client got. RawStatus returns after h does, so a
// recorder inside h has seen the response.
func RawStatus(tb testing.TB, h http.Handler, target string, upgrade bool) int {
	tb.Helper()
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	status := RawGet(tb, server.Listener.Addr().String(), target, upgrade)
	<-done
	return status
}

// RawGet sends one GET for target to the server at addr and returns the
// status the client got. With upgrade set, the request asks to switch
// protocols, as a WebSocket handshake does.
func RawGet(tb testing.TB, addr, target string, upgrade bool) int {
	tb.Helper()
	conn, err := (&net.Dialer{}).DialContext(tb.Context(), "tcp", addr)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	head := "GET " + target + " HTTP/1.1\r\nHost: test\r\n"
	if upgrade {
		head += "Connection: Upgrade\r\nUpgrade: test\r\n"
	}
	if _, err = io.WriteString(conn, head+"\r\n"); err != nil {
		tb.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		tb.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}
