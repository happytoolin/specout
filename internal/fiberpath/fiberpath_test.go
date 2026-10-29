package fiberpath_test

import (
	"testing"

	"github.com/happytoolin/specout/internal/fiberpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAPI(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		names    []string
	}{
		{"/users/:id", "/users/{id}", []string{"id"}},
		{"/:id<int>", "/{id}", []string{"id"}},
		{"/files/*", "/files/{*1...}", []string{"*1"}},
		{"/a/+/b/+", "/a/{+1...}/b/{+2...}", []string{"+1", "+2"}},
		{"/color::color", "/color:{color}", []string{"color"}},
		{`/a\:b`, "/a:b", nil},
		{"/:id<int>-:to", "/{id}-{to}", []string{"id", "to"}},
		{"/:id<int>/x", "/{id}/x", []string{"id"}},
		{`/:id<int>\:x`, "/{id}:x", []string{"id"}},
	} {
		got, names, err := fiberpath.OpenAPI(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
		assert.Equal(t, tc.names, names, tc.in)
	}
}

func TestOpenAPIRejects(t *testing.T) {
	for in, want := range map[string]string{
		"/:id?":     "optional parameter",
		`/a\`:       "trailing backslash",
		"/a/{b}":    "literal braces",
		`/a/\{b`:    "literal braces",
		"/:":        "empty parameter name",
		"/:id<int":  "unclosed constraint",
		"/:id*":     "which specout cannot read",
		"/:id<a>?x": "optional parameter",
		// Fiber matches /5 for these, and gives :a one character of /12.
		"/:id<int>x":   "ignores the text after the constraint",
		"/:id<int>x/y": "ignores the text after the constraint",
		"/:a:b":        "touches the next parameter",
		"/:a<int>:b":   "touches the next parameter",
		"/:a::b":       "touches the next parameter",
	} {
		_, _, err := fiberpath.OpenAPI(in)
		require.ErrorContains(t, err, want, in)
	}
}

func TestTableKeepsUnreadablePaths(t *testing.T) {
	assert.Equal(t, "/{id}", fiberpath.Table("/:id"))
	assert.Equal(t, "/:id?", fiberpath.Table("/:id?"))
}

func TestJoin(t *testing.T) {
	for _, tc := range []struct{ prefix, p, want string }{
		{"/api", "users", "/api/users"},
		{"/api/", "/users", "/api/users"},
		{"/api/", "", "/api/"},
		{"api", "", "/api"},
		{"", "", "/"},
	} {
		assert.Equal(t, tc.want, fiberpath.Join(tc.prefix, tc.p), tc)
	}
}

// guardHint ends each "never runs" error.
const guardHint = "; if that route is a guard that calls c.Next(), skip it with specout.Skip"

// Two routes that Fiber reads the same way are one route, and only the first
// runs. A constraint keeps its case, so \d and \D stay two routes.
func TestShadowedByFold(t *testing.T) {
	never := func(string, string) bool { return false }
	for _, tc := range []struct {
		rd            fiberpath.Reading
		first, second string
		shadowed      bool
	}{
		{fiberpath.Reading{}, "/users/:id", "/Users/:ID/", true},
		{fiberpath.Reading{}, `/A\:b`, `/a\:B`, true},
		{fiberpath.Reading{}, "/files/*", "/Files/*", true},
		{fiberpath.Reading{}, "/:id?", "/:ID?", true},
		{fiberpath.Reading{}, `/a/:x<regex(\d+)>`, `/a/:x<regex(\D+)>`, false},
		{fiberpath.Reading{CaseSensitive: true}, "/users/:id", "/Users/:id", false},
		{fiberpath.Reading{StrictRouting: true}, "/users/:id", "/users/:id/", false},
	} {
		tc.rd.Match = never
		err := fiberpath.Shadowed("pkg", []fiberpath.Route{
			{Method: "GET", Path: tc.first}, {Method: "POST", Path: tc.second}, {Method: "GET", Path: tc.second},
		}, tc.rd)
		if !tc.shadowed {
			require.NoError(t, err, tc.second)
			continue
		}
		require.EqualError(t, err,
			"pkg: GET "+tc.second+" never runs: Fiber sends its requests to GET "+tc.first+", an earlier route"+guardHint)
	}
}

func TestRegexEscapeColon(t *testing.T) {
	for p, want := range map[string]bool{
		`/:id<regex(\d{4})>\:x`:     true,
		`/:a<int>-:b<regex(\d)>\:x`: true,
		`/:id<regex([0-9])>\:x`:     false,
		`/:id<regex(\d)>\-x`:        false,
		`/:id<regex(\d)>/\:x`:       false,
		`/:id<regex(a\:b)>`:         false,
		`/x\:y/:id<int>`:            false,
		`/:id<regex(\d)`:            false,
	} {
		assert.Equal(t, want, fiberpath.RegexEscapeColon(p), p)
	}
}
