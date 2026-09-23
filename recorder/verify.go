package recorder

import (
	"maps"

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
		seen := observed[key]
		for code := range codes {
			if !seen[code] {
				t.Errorf("declared %s %s %d never produced by any test", key.Method, key.Path, code)
			}
		}
	}
	for key, codes := range observed {
		// a declared "default" response (status 0) means any code is in the
		// spec for that route, so nothing the handler writes is drift.
		want := allowed[key]
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
