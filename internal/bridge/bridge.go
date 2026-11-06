// Package bridge connects the specout core, the recorder and the router
// adapters. It is internal, so the hooks stay out of the public API. The
// specout and recorder packages set the hooks when they load, and the
// recorder and the adapters import both, so a hook is always set before a
// caller uses it.
package bridge

import (
	"slices"
	"strings"
)

// Key names one route by method and path, in OpenAPI syntax.
type Key struct{ Method, Path string }

// The hooks. doc is a *specout.Generator and rec a *recorder.Recorder.
var (
	// NewKey returns the key of a route, with router syntax normalized.
	NewKey func(method, path string) Key
	// Statuses returns the status codes of each route. withDefaults adds the
	// codes of Config.DefaultErrors and the ranges.
	Statuses func(doc any, withDefaults bool) (map[Key]map[int]bool, error)
	// Observe records the status of one request to a matched route.
	Observe func(rec any, method, path string, status int)
)

// Skipped reports whether path matches one of skips. A skip that ends in /*
// matches every path below it.
func Skipped(skips []string, path string) bool {
	return slices.ContainsFunc(skips, func(s string) bool {
		if prefix, ok := strings.CutSuffix(s, "*"); ok && strings.HasSuffix(prefix, "/") {
			return strings.HasPrefix(path, prefix)
		}
		return s == path
	})
}

// StrayError names the routes that a router serves without documentation, in
// sorted order.
type StrayError struct{ Routes []string }

func (e *StrayError) Error() string {
	return "specout: routes not documented: " + strings.Join(e.Routes, ", ")
}

// MuxPath translates the path of an http.ServeMux pattern into OpenAPI
// syntax. A path that ends in / serves every path below it, so it becomes the
// catch-all /{*...}. A path that ends in {$} serves itself only.
func MuxPath(p string) string {
	if strings.HasSuffix(p, "/") {
		return p + "{*...}"
	}
	return strings.TrimSuffix(p, "{$}")
}
