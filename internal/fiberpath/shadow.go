package fiberpath

import (
	"errors"
	"fmt"
	"strings"

	utilsstrings "github.com/gofiber/utils/v2/strings"
)

// Route is one endpoint route of a Fiber route table. Added marks a route
// that Fiber adds itself, as Fiber v2's Get adds a HEAD route: it takes the
// requests of later routes, but no error names it as a route that never runs.
type Route struct {
	Method, Path string
	Added        bool
}

// Reading is how one Fiber app reads route paths: its Config.CaseSensitive
// and Config.StrictRouting, and Match, fiber.RoutePatternMatch with that
// Config.
type Reading struct {
	CaseSensitive, StrictRouting bool
	Match                        func(path, pattern string) bool
}

// Shadowed returns an error for each route of table that never runs. table is
// in Fiber's match order: Fiber tries the routes of one method in the order
// they were registered, and the first match runs. A later route never runs
// when an earlier route of its method takes every request it matches:
//
//   - a route without parameters, after a route that matches its path, as
//     /users/me after /users/:id, or /Users after /users with the default
//     Config, which is not case-sensitive;
//   - a route with parameters, after a route that matches its text, as
//     /files/:name after /files/*, or /users/:id after /:a/:b;
//   - any route, after a route that Fiber reads the same way, as /users/:id/
//     after /users/:id with the default Config, which does not use strict
//     routing.
//
// pkg starts each message.
func Shadowed(pkg string, table []Route, rd Reading) error {
	folds := make([]string, len(table))
	for i, r := range table {
		folds[i] = rd.fold(r.Path)
	}
	var errs []error
	for i, r := range table {
		if j, ok := rd.shadow(table[:i], folds, r); ok && !r.Added {
			errs = append(errs, fmt.Errorf("%s: %s %s never runs: Fiber sends its requests to %s %s, %s",
				pkg, r.Method, r.Path, table[j].Method, table[j].Path, table[j].earlier()))
		}
	}
	return errors.Join(errs...)
}

// earlier names r as the route that takes the requests of a later one.
func (r Route) earlier() string {
	if r.Added {
		return "the HEAD route that an earlier Get adds"
	}
	return "an earlier route"
}

// shadow returns the index of the first route of earlier that takes every
// request of r. folds holds the fold of each route.
func (rd Reading) shadow(earlier []Route, folds []string, r Route) (int, bool) {
	fold, path := rd.fold(r.Path), strings.ReplaceAll(r.Path, `\`, "")
	for j, e := range earlier {
		if e.Method == r.Method && (folds[j] == fold || covers(e.Path, r.Path) && rd.Match(path, e.Path)) {
			return j, true
		}
	}
	return 0, false
}

// covers reports whether route earlier takes every request of route later
// when it matches the text of later. A route without parameters matches one
// path: its text. A parameter of later is text that a parameter or wildcard
// of earlier matches, and so is each value of it, unless earlier has a
// constraint, an optional parameter or a literal colon, or later has a
// constraint, a wildcard or an optional parameter. Those match some values and
// not others, more than one segment, or no segment.
func covers(earlier, later string) bool {
	if _, names, err := OpenAPI(later); err == nil && len(names) == 0 {
		return true
	}
	return !strings.ContainsAny(earlier, `<?\`) && !strings.Contains(earlier, "::") &&
		!strings.ContainsAny(later, "<*+?")
}

// fold returns route path p as rd reads it: in lowercase unless the app is
// case-sensitive, and without a trailing slash unless its routing is strict.
// A constraint keeps its case, as in Fiber. Two routes with one fold are one
// route to Fiber.
func (rd Reading) fold(p string) string {
	if !rd.StrictRouting && len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if rd.CaseSensitive {
		return p
	}
	// Fiber's ASCII lowercase keeps each byte at its offset, so a constraint
	// takes its own case back from p.
	b := []byte(utilsstrings.ToLower(p))
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '<':
			if end := constraintEnd(p, i+1); end >= 0 {
				copy(b[i:end], p[i:end])
				i = end
			}
		}
	}
	return string(b)
}
