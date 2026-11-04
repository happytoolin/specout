package recorder

import (
	"cmp"
	"maps"
	"net/http"
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
	observed, misses := rec.snapshot()
	unmatched := rec.matchMisses(observed, allowed, misses)

	// Requests went through, but none reached a known route: middleware or a
	// prefix outside the router hides its routes.
	if len(observed) == 0 && unmatched > 0 && len(required) > 0 {
		t.Errorf("recorder matched no route in %d requests; pass the router itself to recorder.New, "+
			"or wrap the recorder in the middleware, as in middleware(recorder.New(router))", unmatched)
	}
	reportNeverProduced(t, required, observed)
	reportUndeclared(t, allowed, observed)
}

// snapshot copies the observed codes and the requests that matched no route.
func (rec *Recorder) snapshot() (statuses, []miss) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	observed := make(statuses, len(rec.codes))
	for key, codes := range rec.codes {
		observed[key] = maps.Clone(codes)
	}
	return observed, slices.Clone(rec.misses)
}

// matchMisses adds each request that matched no route to observed, under the
// documented route that its path matches. Middleware outside the router, such
// as an auth check that answers 401, hides the route from the recorder, but
// the request was for that route. A 404 or 405 is the answer of a router that
// found no route, so it counts for no route. It returns the count of requests
// that match no route.
func (rec *Recorder) matchMisses(observed, documented statuses, misses []miss) int {
	unmatched := 0
	for _, m := range misses {
		key, ok := matchRoute(documented, m.method, m.path)
		switch {
		case !ok, m.status == http.StatusNotFound, m.status == http.StatusMethodNotAllowed:
			unmatched++
		case slices.ContainsFunc(rec.skips, func(s specout.SkipRule) bool { return s.Matches(key.Path) }):
		default:
			if observed[key] == nil {
				observed[key] = map[int]bool{}
			}
			observed[key][m.status] = true
		}
	}
	return unmatched
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
