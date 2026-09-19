package recorder

import (
	"maps"
	"net/http"

	"github.com/happytoolin/specout"
)

// Verify fails the test when declared and observed status codes disagree in
// either direction, naming the route and code. required is what each route
// declares for itself, so a code never produced there is a coverage gap;
// allowed adds the global error envelope, so a handler that returns a declared
// default is not drift.
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
	rec.mu.Lock()
	observed := maps.Clone(rec.codes)
	for key, codes := range observed {
		observed[key] = maps.Clone(codes)
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
		// a declared "default" response (status 0) means any code is in the
		// spec for that route, so nothing the handler writes is drift.
		want := lookupKey(allowed, key)
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
