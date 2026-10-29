// Package adaptertest is the test plumbing the router adapter tests share. It
// is internal to this repo and is never part of the library.
package adaptertest

import (
	"errors"
	"fmt"
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
