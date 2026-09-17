package main

import "github.com/happytoolin/specout"

// errs is the published failure pair: the 4XX/5XX ranges, described "error".
var errs = []specout.Response{
	{Key: "4XX", Type: ODataError{}, Raw: map[string]any{"description": "error"}},
	{Key: "5XX", Type: ODataError{}, Raw: map[string]any{"description": "error"}},
}

// withErrs joins one operation's own responses with the shared failure pair.
func withErrs(rs ...specout.Response) []specout.Response { return append(rs, errs...) }

// ok drops the Res-derived 200 body and re-keys the success under 2XX.
func ok(description string) []specout.Response {
	return withErrs(
		specout.Response{Status: 200, Omit: true},
		specout.Response{Status: 200, Key: "2XX", Raw: map[string]any{"description": description}},
	)
}

// noContent is the published 204 — the concrete code Graph keeps, with no body.
func noContent() []specout.Response {
	return withErrs(specout.Response{Status: 204, Type: struct{}{}, Raw: map[string]any{"description": "Success"}})
}
