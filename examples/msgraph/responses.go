package main

import "github.com/happytoolin/specout"

// errs is the published failure pair: the 4XX/5XX ranges, described "error".
var errs = []specout.Response{
	{Key: "4XX", Type: ODataError{}, Description: "error"},
	{Key: "5XX", Type: ODataError{}, Description: "error"},
}

// withErrs joins one operation's own responses with the shared failure pair.
func withErrs(rs ...specout.Response) []specout.Response { return append(rs, errs...) }

// ok keys the success under 2XX. It is a declared success response, so it
// replaces the Res-derived 200.
func ok(description string) []specout.Response {
	return withErrs(specout.Response{Status: 200, Key: "2XX", Description: description})
}

// noContent is the published 204: the concrete code Graph keeps, with no body.
func noContent() []specout.Response {
	return withErrs(specout.Response{Status: 204, Description: "Success"})
}
