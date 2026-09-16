package specout

// Response declares one HTTP status code for an operation.
//
// Type omitted = inherit the handler Res type at this code; 204 and 304 are
// the exception, since HTTP forbids a body there. ContentType set
// = a binary/raw body (schema format binary), e.g. "application/pdf".
// Headers declares response headers for this code; Raw splices arbitrary
// OpenAPI fragments into the response object.
type Response struct {
	Status      int
	Type        any
	ContentType string
	Headers     []Header
	Raw         map[string]any
	Omit        bool
	// ContentTypes emits this response body under more than one media type
	// with the one schema (["application/json", "application/xml"]). Use
	// ContentType for a single binary body instead.
	ContentTypes []string
	// Key names the response entry instead of the status number: "default"
	// (same as Status 0) or a range, "4XX". Published documents key one
	// response for a whole range (MS Graph does it 17870 times); Status then
	// stays 0 or names one concrete code for coverage.
	Key string
}

// Header declares one response header. Type carries the header's schema:
// specout.Header{Name: "Location"} is a plain string; pass any struct or
// scalar type to give it a real schema (registered as a component).
type Header struct {
	Name string
	Type any
}

// File as a request field marks a file upload: the request body becomes
// multipart/form-data and the field schema is format binary. Declaration
// only; the handler reads the multipart body itself.
type File struct{}

// NoContent as Res means "204, no body" - Go's own empty marker, kept as a
// named type so signatures read clearly.
type NoContent struct{}
