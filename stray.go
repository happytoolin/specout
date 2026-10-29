package specout

import (
	"net/http"
	"strings"
)

// strayScan collects the routes an adapter's walk reports that specout cannot
// document. Every Adopt and CheckRoutes reports through it: they differ only
// in how they ask their router for one entry at a time.
type strayScan struct {
	d       *Generator
	skips   []SkipRule
	unknown []string
	seen    map[string]bool
}

// collect takes one router entry. methods is the joined method list, or "" when
// the router serves this handler on every method: chi reports such a route once
// per method, gorilla as a route with no method constraint, and neither is one
// OpenAPI operation.
func (s *strayScan) collect(pattern, methods string, h http.Handler) {
	for _, sk := range s.skips {
		if sk.Matches(pattern) {
			return
		}
	}
	// the spec itself is usually served from the router it documents
	if h == http.Handler(s.d) {
		return
	}
	if methods == "" {
		s.report("? " + pattern + " (no method constraint)")
		return
	}
	for method := range strings.SplitSeq(methods, ",") {
		if !s.d.lookup(method, pattern, h) {
			s.report(method + " " + pattern)
		}
	}
}

// report names one stray once: a router table can list one route twice, and
// a repeated name only hides the next one.
func (s *strayScan) report(route string) {
	if s.seen[route] {
		return
	}
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.seen[route] = true
	s.unknown = append(s.unknown, route)
}

func (s *strayScan) err() error {
	if len(s.unknown) == 0 {
		return nil
	}
	return &StrayError{Routes: s.unknown}
}

// StrayError names routes registered without specout metadata.
type StrayError struct{ Routes []string }

func (e *StrayError) Error() string {
	return "specout: routes not documented: " + strings.Join(e.Routes, ", ")
}
