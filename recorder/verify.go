package recorder

import (
	"cmp"
	"maps"
	"slices"

	"github.com/happytoolin/specout"
)

// statuses maps each route to a set of status codes.
type statuses = map[specout.RouteKey]map[int]bool

// Verify fails the test when declared and observed status codes disagree in
// either direction, naming the route and code. required is what each route
// declares for itself, so a code never produced there is a coverage gap;
// allowed adds the global error envelope, so a handler that returns a declared
// default is not drift. Failures come in path, method and code order.
func Verify(t specout.TestingT, d *specout.Generator, rec *Recorder) {
	t.Helper()
	required, err := d.DeclaredStatuses()
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	allowed, err := d.SpecStatuses()
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	observed, unmatched := rec.snapshot()

	// Requests went through, but none reached a known route: the recorder
	// most likely wraps middleware around the router, which hides its routes.
	if len(observed) == 0 && unmatched > 0 && len(required) > 0 {
		t.Errorf("recorder matched no route in %d requests; pass the router itself to recorder.New, "+
			"and put middleware inside it", unmatched)
	}
	reportNeverProduced(t, required, observed)
	reportUndeclared(t, allowed, observed)
}

// snapshot copies the observed codes and the unmatched count.
func (rec *Recorder) snapshot() (statuses, int) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	observed := make(statuses, len(rec.codes))
	for key, codes := range rec.codes {
		observed[key] = maps.Clone(codes)
	}
	return observed, rec.unmatched
}

// reportNeverProduced fails for each declared code that no request produced.
func reportNeverProduced(t specout.TestingT, required, observed statuses) {
	t.Helper()
	for _, key := range sortedRoutes(required) {
		seen := observed[key]
		for _, code := range slices.Sorted(maps.Keys(required[key])) {
			if !seen[code] {
				t.Errorf("declared %s %s %d never produced by any test", key.Method, key.Path, code)
			}
		}
	}
}

// reportUndeclared fails for each code a handler wrote that the spec does not
// declare.
func reportUndeclared(t specout.TestingT, allowed, observed statuses) {
	t.Helper()
	for _, key := range sortedRoutes(observed) {
		// a declared "default" response (status 0) means any code is in the
		// spec for that route, so nothing the handler writes is drift.
		want := allowed[key]
		if want[0] {
			continue
		}
		for _, code := range slices.Sorted(maps.Keys(observed[key])) {
			if !want[code] {
				t.Errorf("handler wrote %s %s %d but spec does not declare it", key.Method, key.Path, code)
			}
		}
	}
}

// sortedRoutes returns the routes of m by path, then method.
func sortedRoutes(m statuses) []specout.RouteKey {
	return slices.SortedFunc(maps.Keys(m), func(a, b specout.RouteKey) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
}
