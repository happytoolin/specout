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
