package specout

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5"
)

// TestingT is the subset of *testing.T RequireDocumented needs.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
}

// SkipRule allows specific patterns in RequireDocumented.
type SkipRule struct{ pattern string }

// Skip builds a SkipRule for a chi wildcard pattern like "/debug/*".
func Skip(pattern string) SkipRule { return SkipRule{pattern: pattern} }

// RouteKey identifies one method+path for examples and recorder data.
type RouteKey struct{ Method, Path string }

// routerGenerators maps each chi router to the generator that adopted it.
// ponytail: package-level state; the frozen RequireDocumented signature has
// no generator param, so the link must live somewhere. Revisit if this
// blocks multi-generator tests.
var routerGenerators = map[any]*Generator{}

func linkRouter(r chiRouter, d *Generator) { routerGenerators[r] = d }

// RequireDocumented fails the test when the chi router holds endpoint routes
// specout never registered (stray plain handlers). Std-mux strays are
// invisible — no enumeration API; convention plus review covers them.
func RequireDocumented(t TestingT, r chi.Router, skips ...SkipRule) {
	t.Helper()
	d := routerGenerators[r]
	var missing []string
	err := chi.Walk(r, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		for _, s := range skips {
			if matchSkip(s.pattern, route) {
				return nil
			}
		}
		if d == nil {
			return nil
		}
		ptr := reflect.ValueOf(handler).Pointer()
		if _, ok := d.lookup(ptr); !ok {
			missing = append(missing, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Errorf("walk: %v", err)
	}
	if len(missing) > 0 {
		t.Errorf("routes not documented: %s", strings.Join(missing, ", "))
	}
}

func matchSkip(pattern, route string) bool {
	if strings.HasSuffix(pattern, "/*") {
		return strings.HasPrefix(route, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == route
}
