package specout

import "net/http"

// Handler is an http.HandlerFunc with operation metadata. Req and Res give
// the request and response schemas. The handler itself is a plain
// net/http handler.
type Handler[Req, Res any] struct {
	http.HandlerFunc

	Responses   []Response
	Tags        []string
	Summary     string
	OperationID string
	Description string
	// ExternalDocs links this operation to outside documentation.
	ExternalDocs *ExternalDocs
	// Request describes the request body. Req gives its schema.
	Request Request
	// Security replaces the document's auth requirement for this operation.
	// Each entry names a scheme from Config.Auth, and any one of them
	// satisfies the requirement.
	Security []Security
	// Servers replaces the document's servers for this operation.
	Servers []Server
	// Callbacks documents the requests that your API sends back because of this
	// operation. Make each one with NewCallback.
	Callbacks []Callback
	// Raw splices keys into this operation object: x- extensions, or a whole
	// field that replaces what specout writes, such as responses. A field with
	// a typed form, such as callbacks, panics.
	Raw        map[string]any
	Deprecated bool
	Public     bool // exclude this operation from the global auth requirement
}

// Request describes the request body of an operation, as Response describes a
// response. A Req with no body panics when any field is set.
type Request struct {
	// ContentTypes replaces the media types of the body, such as
	// "application/x-www-form-urlencoded". Form media types use the form field
	// names of Req, and other media types use its JSON field names.
	ContentTypes []string
	Description  string
	// Optional marks the body as optional. It is required by default.
	Optional bool
	// Examples are examples of the body, by name.
	Examples map[string]Example
	// Encoding describes fields of a form or multipart body, by field name. The
	// body must be a form, and each name must be one of its fields.
	Encoding map[string]Encoding
}

// Security names one scheme of Config.Auth that an operation accepts. Scopes
// lists the OAuth 2.0 scopes or the roles that the operation needs. Make one
// with AuthScheme.Require.
type Security struct {
	Scheme string
	Scopes []string
}
