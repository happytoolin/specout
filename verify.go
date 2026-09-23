package specout

import "strings"

// TestingT is the subset of *testing.T the verification helpers need.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
}

// SkipRule allows specific patterns in Adopt/RequireDocumented.
type SkipRule struct{ pattern string }

// Skip builds a SkipRule for a wildcard pattern like "/debug/*".
func Skip(pattern string) SkipRule { return SkipRule{pattern: pattern} }

// Matches reports whether route matches this rule.
func (s SkipRule) Matches(route string) bool { return matchSkip(s.pattern, route) }

// RouteKey identifies one method+path for the recorder drift check.
type RouteKey struct{ Method, Path string }

// NewRouteKey normalizes router syntax while preserving distinct paths,
// including trailing slashes, for the recorder drift check.
func NewRouteKey(method, pattern string) RouteKey {
	return RouteKey{Method: method, Path: docPath(canonicalPath(pattern))}
}

func matchSkip(pattern, route string) bool {
	if strings.HasSuffix(pattern, "/*") {
		return strings.HasPrefix(route, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == route
}

// requireDocumented is the shared body of every adapter's RequireDocumented:
// an Adopt error is a test failure, never a panic.
func requireDocumented(t TestingT, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%v", err)
	}
}
