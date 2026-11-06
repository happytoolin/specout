package recorder

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/bridge"
)

type statuses = map[bridge.Key]map[int]bool

// Verify returns an error when the declared and the recorded status codes
// disagree in either direction. A code that a route declares for itself and
// that no request produced is a coverage gap. A recorded code that the
// document does not list is drift; a code of Config.DefaultErrors is listed.
// The error names each route and code, in path, method and code order.
func Verify(d *specout.Generator, rec *Recorder) error {
	required, err := bridge.Statuses(d, false)
	if err != nil {
		return err
	}
	allowed, err := bridge.Statuses(d, true)
	if err != nil {
		return err
	}
	observed, misses := rec.snapshot()
	unmatched := rec.matchMisses(observed, allowed, misses)

	var errs []error
	// Requests went through, but none reached a known route: middleware or a
	// prefix outside the router hides its routes.
	if len(observed) == 0 && unmatched > 0 && len(required) > 0 {
		errs = append(errs, fmt.Errorf("recorder matched no route in %d requests; pass the router itself to "+
			"recorder.New, or wrap the recorder in the middleware, as in middleware(recorder.New(router))", unmatched))
	}
	errs = append(errs, neverProduced(required, observed)...)
	errs = append(errs, undeclared(allowed, observed)...)
	return errors.Join(errs...)
}

// snapshot copies the observed codes and the requests that matched no route.
func (rec *Recorder) snapshot() (statuses, map[miss]int) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	observed := make(statuses, len(rec.codes))
	for key, codes := range rec.codes {
		observed[key] = maps.Clone(codes)
	}
	return observed, maps.Clone(rec.misses)
}

// matchMisses adds each request that matched no route to observed, under the
// documented route that its path matches. Middleware outside the router, such
// as an auth check that answers 401, hides the route from the recorder, but
// the request was for that route. A 404 or 405 is the answer of a router that
// found no route, so it counts for no route. It returns the count of requests
// that match no route.
func (rec *Recorder) matchMisses(observed, documented statuses, misses map[miss]int) int {
	unmatched := 0
	for m, n := range misses {
		key, ok := matchRoute(documented, m.method, m.path)
		switch {
		case !ok, m.status == http.StatusNotFound, m.status == http.StatusMethodNotAllowed:
			unmatched += n
		case bridge.Skipped(rec.skips, key.Path):
		default:
			if observed[key] == nil {
				observed[key] = map[int]bool{}
			}
			observed[key][m.status] = true
		}
	}
	return unmatched
}

// neverProduced returns an error for each code that a route declares and that
// no request produced.
func neverProduced(required, observed statuses) []error {
	var errs []error
	for _, key := range sortedRoutes(required) {
		seen := observed[key]
		for _, code := range slices.Sorted(maps.Keys(required[key])) {
			if !seen[code] {
				errs = append(errs, fmt.Errorf("declared %s %s %d never produced by any test", key.Method, key.Path, code))
			}
		}
	}
	return errs
}

// undeclared returns an error for each code a handler wrote that the spec
// does not declare.
func undeclared(allowed, observed statuses) []error {
	var errs []error
	for _, key := range sortedRoutes(observed) {
		// a declared "default" response (status 0) means any code is in the
		// spec for that route, so nothing the handler writes is drift.
		want := allowed[key]
		if want[0] {
			continue
		}
		for _, code := range slices.Sorted(maps.Keys(observed[key])) {
			if !want[code] {
				errs = append(errs, fmt.Errorf("handler wrote %s %s %d but spec does not declare it", key.Method, key.Path, code))
			}
		}
	}
	return errs
}

func sortedRoutes(m statuses) []bridge.Key {
	return slices.SortedFunc(maps.Keys(m), func(a, b bridge.Key) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
}
