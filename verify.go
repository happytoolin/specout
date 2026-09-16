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

func matchSkip(pattern, route string) bool {
	if strings.HasSuffix(pattern, "/*") {
		return strings.HasPrefix(route, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == route
}
