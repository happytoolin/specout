package recorder

import "github.com/happytoolin/specout"

// TestingT is the subset of *testing.T Verify needs.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Verify fails the test when declared and observed status codes disagree in
// either direction, naming the route and code.
func Verify(t TestingT, d *specout.Generator, rec *Recorder) {
	t.Helper()
	declared := d.DeclaredStatuses()
	rec.mu.Lock()
	observed := make(map[specout.RouteKey]map[int]bool, len(rec.codes))
	for k, v := range rec.codes {
		observed[k] = v
	}
	rec.mu.Unlock()

	for key, codes := range declared {
		seen := observed[key]
		for code := range codes {
			if !seen[code] {
				t.Errorf("declared %s %s %d never produced by any test", key.Method, key.Path, code)
			}
		}
	}
	for key, codes := range observed {
		want := declared[key]
		for code := range codes {
			if !want[code] {
				t.Errorf("handler wrote %s %s %d but spec does not declare it", key.Method, key.Path, code)
			}
		}
	}
}
