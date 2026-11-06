package specout

// Response declares one HTTP status code for an operation.
//
// Set Status, or Key for a range or the default response. With no declared
// success response, the operation gets 200 with the Res body, or 204 when Res
// has no body. A success response is a 2xx, a redirect (a 3xx other than 304),
// or a 101. So a declared 201 replaces the 200, and a declared 404 keeps it.
//
// A HEAD operation documents no content at all. Type: File{} is a binary body.
// Headers declares response headers for this code. Raw splices x- extensions,
// or a field that has no typed form, into the response object.
type Response struct {
	Status int
	// Type is the body. With no Type, a 2xx has the body of Res, and a 4xx,
	// 5xx or default response has the body of Config.ErrorType. Any other
	// response has no body. Set Type: NoContent{} for a response with no
	// body. 1xx, 204, 205 and 304 never have a body.
	Type any
	// Description replaces the status text, such as "Not Found".
	Description string
	Headers     []Header
	// Examples are examples of the body, by name. They need a body.
	Examples map[string]Example
	// Links tells a client how to use this response in a next request, by link
	// name.
	Links map[string]Link
	Raw   map[string]any
	// Omit removes the response at this status or Key: the inferred 200 or
	// 204, or a status of Config.DefaultErrors.
	Omit bool
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

// NoContent as Res means "204, no body". As Response.Type, it means that the
// response has no body.
type NoContent struct{}
