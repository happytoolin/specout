package main

import "github.com/happytoolin/specout"

// ok and okJSON override the description of the Res-derived 200: ok is the
// published JSON + XML pair, okJSON the operations that answer JSON only.
func ok(description string) specout.Response {
	return specout.Response{Status: 200, ContentTypes: jsonOrXML, Raw: map[string]any{"description": description}}
}

func okJSON(description string) specout.Response {
	return specout.Response{Status: 200, Raw: map[string]any{"description": description}}
}

// desc is a description-only response: the published document declares its
// errors and its few body-less 200s this way; def is its "default" catch-all.
func desc(code int, description string) specout.Response {
	return specout.Response{Status: code, Type: struct{}{}, Raw: map[string]any{"description": description}}
}

func def(description string) specout.Response { return desc(0, description) }

var (
	jsonOrXML   = []string{"application/json", "application/xml"}
	jsonXMLForm = []string{"application/json", "application/xml", "application/x-www-form-urlencoded"}

	// The published tag of each group, then the answers more than one operation
	// declares: one variable per published description.
	tagPet, tagStore, tagUser = []string{"pet"}, []string{"store"}, []string{"user"}

	unexpected      = def("Unexpected error")
	invalidInput    = desc(400, "Invalid input")
	invalidID       = desc(400, "Invalid ID supplied")
	invalidUsername = desc(400, "Invalid username supplied")
	validation      = desc(422, "Validation exception")
	petNotFound     = desc(404, "Pet not found")
	orderNotFound   = desc(404, "Order not found")
	userNotFound    = desc(404, "User not found")
)
