package echopath_test

import (
	"testing"

	"github.com/happytoolin/specout/internal/echopath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAPI(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		names    []string
	}{
		{"/users/:id", "/users/{id}", []string{"id"}},
		{"users", "/users", nil},
		{`/items/:id\:cancel`, "/items/{id}:cancel", []string{"id"}},
		{`/a\:b`, "/a:b", nil},
		{"/files/*", "/files/{*...}", []string{"*"}},
		{"/files/*/ignored", "/files/{*...}", []string{"*"}},
	} {
		got, names, err := echopath.OpenAPI(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
		assert.Equal(t, tc.names, names, tc.in)
	}
}

func TestOpenAPIRejects(t *testing.T) {
	for in, want := range map[string]string{
		"/a/{b}": "literal braces",
		"/:a-:b": "cannot be an OpenAPI parameter",
		"/:":     "cannot be an OpenAPI parameter",
	} {
		_, _, err := echopath.OpenAPI(in)
		require.ErrorContains(t, err, want, in)
	}
}

func TestTableKeepsUnreadablePaths(t *testing.T) {
	assert.Equal(t, "/{id}", echopath.Table("/:id"))
	assert.Equal(t, "/a/{b}", echopath.Table("/a/{b}"))
}
