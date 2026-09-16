package specout

import (
	"net/http"
	"reflect"
)

// Handler is an http.HandlerFunc factory product carrying request and
// response schema metadata in its type parameters. The embedded
// http.HandlerFunc stays a plain std handler; the closure sees (w, r) and
// nothing else.
type Handler[Req, Res any] struct {
	http.HandlerFunc
	Responses   []Response
	Tags        []string
	Summary     string
	OperationID string
	Description string
	// ExternalDocs links this operation to outside documentation.
	ExternalDocs *ExternalDocs
	// RequestContentTypes overrides the request body media types
	// (application/x-www-form-urlencoded, application/xml, ...). The schema
	// still comes from Req; only the media type changes. One entry per media
	// type: two entries publish one shape under both.
	RequestContentTypes []string
	// Raw splices arbitrary keys into this operation object: x- extensions,
	// or any other OpenAPI operation field specout does not model.
	Raw        map[string]any
	Deprecated bool
	Public     bool // exclude this operation from the global auth requirement
}

// Types reifies the type parameters, making them visible to reflection at
// registration time. It is the reification point; no AST analysis, ever.
func (h Handler[Req, Res]) Types() (req, res reflect.Type) {
	return reflect.TypeFor[Req](), reflect.TypeFor[Res]()
}

// Documented is implemented by metadata-carrying handlers. Registration
// helpers type-assert against it.
type Documented interface {
	Types() (req, res reflect.Type)
}
