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
func (h Handler[Req, Res]) Types() (reflect.Type, reflect.Type) {
	return reflect.TypeFor[Req](), reflect.TypeFor[Res]()
}


// With* methods are the fluent form of the metadata fields: each returns a
// copy of h with the field set, so chains never mutate. Tags and Responses
// append; the rest replace. Field assignment stays equally valid — one
// vocabulary, two spellings.

// WithSummary sets the operation summary.
func (h Handler[Req, Res]) WithSummary(summary string) Handler[Req, Res] {
	h.Summary = summary
	return h
}

// WithDescription sets the long description.
func (h Handler[Req, Res]) WithDescription(desc string) Handler[Req, Res] {
	h.Description = desc
	return h
}

// WithOperationID sets an explicit operationId.
func (h Handler[Req, Res]) WithOperationID(id string) Handler[Req, Res] {
	h.OperationID = id
	return h
}

// WithTags appends tags.
func (h Handler[Req, Res]) WithTags(tags ...string) Handler[Req, Res] {
	h.Tags = append(h.Tags, tags...)
	return h
}

// WithResponse appends one response declaration.
func (h Handler[Req, Res]) WithResponse(r Response) Handler[Req, Res] {
	h.Responses = append(h.Responses, r)
	return h
}

// WithResponses appends several response declarations.
func (h Handler[Req, Res]) WithResponses(rs ...Response) Handler[Req, Res] {
	h.Responses = append(h.Responses, rs...)
	return h
}

// WithRequestContentTypes appends request body media types.
func (h Handler[Req, Res]) WithRequestContentTypes(cts ...string) Handler[Req, Res] {
	h.RequestContentTypes = append(h.RequestContentTypes, cts...)
	return h
}

// WithExternalDocs sets the operation-level external docs.
func (h Handler[Req, Res]) WithExternalDocs(ed *ExternalDocs) Handler[Req, Res] {
	h.ExternalDocs = ed
	return h
}

// WithRaw splices operation-level keys (x- extensions); it replaces, not merges.
func (h Handler[Req, Res]) WithRaw(raw map[string]any) Handler[Req, Res] {
	h.Raw = raw
	return h
}

// WithDeprecated marks the operation deprecated.
func (h Handler[Req, Res]) WithDeprecated() Handler[Req, Res] {
	h.Deprecated = true
	return h
}

// WithPublic opts the operation out of the global auth requirement.
func (h Handler[Req, Res]) WithPublic() Handler[Req, Res] {
	h.Public = true
	return h
}
