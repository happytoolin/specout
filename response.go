package specout

// Response declares one HTTP status code for an operation.
//
// Set Status, or Key for a range or the default response. Type omitted = inherit
// the handler Res type at this code; 1xx, 204, 205 and 304 are the exception,
// since HTTP forbids a body there, and a Type with fields or ContentTypes on
// them panics. A HEAD operation documents no content at all. Type: File{} is a
// binary body. Headers declares response headers for this code; Raw splices x- extensions,
// or a whole field that replaces what specout writes, into the response object.
type Response struct {
	Status int
	Type   any
	// Description replaces the status text, such as "Not Found".
	Description string
	Headers     []Header
	// Example is one example of the body. Use Examples for several named
	// examples. Set one of them. Both need a body.
	Example  any
	Examples map[string]Example
	// Links tells a client how to use this response in a next request, by link
	// name.
	Links map[string]Link
	Raw   map[string]any
	Omit  bool
	// ContentTypes replaces the media types of this body. The schema stays the
	// same, so use it for ["application/json", "application/xml"], or with
	// Type: File{} for ["application/pdf"]. A File body is
	// application/octet-stream by default.
	ContentTypes []string
	// Key names the response entry instead of the status number: "default" or
	// a range, "4XX". Published documents key one
	// response for a whole range (MS Graph does it 17870 times); Status then
	// stays 0 or names one concrete code for coverage.
	Key string
}

// Header declares one response header. Type carries the header's schema:
// specout.Header{Name: "Location"} is a plain string; pass any struct or
// scalar type to give it a real schema (registered as a component).
type Header struct {
	Name        string
	Type        any
	Description string
	Required    bool
	Deprecated  bool
}

// File is a binary payload. As a request field, it marks a file upload: the
// request body becomes multipart/form-data and the field schema is format
// binary. As Req, or as Res or Response.Type, it is the whole body. Declaration
// only; the handler reads and writes the bytes itself.
type File struct{}

// NoContent as Res means "204, no body" - Go's own empty marker, kept as a
// named type so signatures read clearly.
type NoContent struct{}
