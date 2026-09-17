// Package exampletest is the test plumbing the specout examples share. It is
// internal to this repo and is never part of the library.
package exampletest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/happytoolin/specout"
)

// CaptureT collects failures so a test can assert on one drift category
// without the other failing it.
type CaptureT struct{ Errs []string }

func (c *CaptureT) Helper()                   {}
func (c *CaptureT) Errorf(f string, a ...any) { c.Errs = append(c.Errs, fmt.Sprintf(f, a...)) }
func (c *CaptureT) Fatalf(f string, a ...any) { c.Errs = append(c.Errs, fmt.Sprintf(f, a...)) }

// Spec renders d and returns the parsed document.
func Spec(t testing.TB, d *specout.Generator) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// Dig walks a decoded document by key path and fails the test at the first
// level the published shape is missing.
func Dig(t testing.TB, v any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("no object at %q: %v", k, v)
		}
		if v, ok = m[k]; !ok {
			t.Fatalf("no %q in %v", k, m)
		}
	}
	return v
}

// Obj is Dig ending on a JSON object.
func Obj(t testing.TB, v any, keys ...string) map[string]any {
	t.Helper()
	return Dig(t, v, keys...).(map[string]any)
}
