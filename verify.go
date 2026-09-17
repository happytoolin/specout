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

// NewRouteKey canonicalizes a router pattern into the key the drift check
// compares. Regex constraints are stripped like an OpenAPI template, and one
// trailing slash is trimmed: chi reports /a and /a/ as one RoutePattern, so
// the served side must meet the declared side there. Registered document
// paths keep their exact form — /a and /a/ are distinct OpenAPI paths.
func NewRouteKey(method, pattern string) RouteKey {
	p := docPath(pattern)
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return RouteKey{Method: method, Path: p}
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
