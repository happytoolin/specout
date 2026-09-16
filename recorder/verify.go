package recorder

import (
	"net/http"

	"github.com/happytoolin/specout"
)

// TestingT is the subset of *testing.T Verify needs.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Verify fails the test when declared and observed status codes disagree
// in either direction, naming the route and code. Resolution failures in
// the generator are reported, not swallowed.
func Verify(t TestingT, d *specout.Generator, rec *Recorder) {
	t.Helper()
	// required: what each route declares for itself; a code never produced
	// there is a coverage gap.
	required, err := d.DeclaredStatuses()
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	// allowed: the full spec set, including the global error envelope; a
	// handler that returns a declared default is not drift.
	allowed, err := d.SpecStatuses()
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	rec.mu.Lock()
	observed := make(map[specout.RouteKey]map[int]bool, len(rec.codes))
	for k, v := range rec.codes {
		observed[k] = v
	}
	rec.mu.Unlock()

	for key, codes := range required {
		seen := lookupKey(observed, key)
		for code := range codes {
			if !seen[code] {
				t.Errorf("declared %s %s %d never produced by any test", key.Method, key.Path, code)
			}
		}
	}
	for key, codes := range observed {
		want := lookupKey(allowed, key)
		// a declared "default" response (status 0) means any code is in the
		// spec for that route, so nothing the handler writes is drift.
		if want[0] {
			continue
		}
		for code := range codes {
			if !want[code] {
				t.Errorf("handler wrote %s %s %d but spec does not declare it", key.Method, key.Path, code)
			}
		}
	}
}

// lookupKey accepts HEAD and GET as the same route: net/http serves HEAD
// through the GET handler, so either observation satisfies the other.
func lookupKey(m map[specout.RouteKey]map[int]bool, k specout.RouteKey) map[int]bool {
	if v := m[k]; v != nil {
		return v
	}
	switch k.Method {
	case http.MethodHead:
		return m[specout.RouteKey{Method: http.MethodGet, Path: k.Path}]
	case http.MethodGet:
		return m[specout.RouteKey{Method: http.MethodHead, Path: k.Path}]
	}
	return nil
}
