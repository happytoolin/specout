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
	Responses  []Response
	Tags       []string
	Summary    string
	Deprecated bool
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
