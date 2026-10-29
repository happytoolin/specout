package fiberpath

import (
	"errors"
	"fmt"
	"strings"
)

// Route is one endpoint route of a Fiber route table.
type Route struct{ Method, Path string }

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
		if j, ok := rd.shadow(table[:i], folds, r); ok {
			errs = append(errs, fmt.Errorf("%s: %s %s never runs: Fiber sends its requests to %s %s, an earlier route",
				pkg, r.Method, r.Path, table[j].Method, table[j].Path))
		}
	}
	return errors.Join(errs...)
}

// shadow returns the index of the first route of earlier that takes every
// request of r. folds holds the fold of each route.
func (rd Reading) shadow(earlier []Route, folds []string, r Route) (int, bool) {
	fold := rd.fold(r.Path)
	// A route without parameters or wildcards matches one path: the route
	// read as a request. A route with them matches more paths than any one
	// request shows, so only a route that Fiber reads the same way takes all.
	_, names, err := OpenAPI(r.Path)
	static, path := err == nil && len(names) == 0, strings.ReplaceAll(r.Path, `\`, "")
	for j, e := range earlier {
		if e.Method == r.Method && (folds[j] == fold || static && rd.Match(path, e.Path)) {
			return j, true
		}
	}
	return 0, false
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
	b := []byte(p)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\\':
			// the escaped character is still lowercase to Fiber
			if i+1 < len(b) {
				i++
				b[i] = lower(b[i])
			}
		case '<':
			if end := constraintEnd(p, i+1); end >= 0 {
				i = end
			}
		default:
			b[i] = lower(b[i])
		}
	}
	return string(b)
}

// lower returns the lowercase form of an ASCII letter c, and any other byte
// unchanged, as Fiber lowercases route paths.
func lower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
