package specout

import (
	"net/http"
	"slices"
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
	// Example is one example of the body. Use Examples for several named
	// examples. Set one of them.
	Example  any
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
	h.Tags = append(slices.Clone(h.Tags), tags...)
	return h
}

// WithResponse appends one response declaration.
func (h Handler[Req, Res]) WithResponse(r Response) Handler[Req, Res] {
	return h.WithResponses(r)
}

// WithResponses appends several response declarations.
func (h Handler[Req, Res]) WithResponses(rs ...Response) Handler[Req, Res] {
	h.Responses = append(slices.Clone(h.Responses), rs...)
	return h
}

// WithRequest describes the request body.
func (h Handler[Req, Res]) WithRequest(request Request) Handler[Req, Res] {
	h.Request = request
	return h
}

// WithServers appends servers that replace the document's for this operation.
func (h Handler[Req, Res]) WithServers(servers ...Server) Handler[Req, Res] {
	h.Servers = append(slices.Clone(h.Servers), servers...)
	return h
}

// WithCallbacks appends requests that your API sends because of this operation.
func (h Handler[Req, Res]) WithCallbacks(callbacks ...Callback) Handler[Req, Res] {
	h.Callbacks = append(slices.Clone(h.Callbacks), callbacks...)
	return h
}

// WithSecurity appends schemes that this operation accepts.
func (h Handler[Req, Res]) WithSecurity(security ...Security) Handler[Req, Res] {
	h.Security = append(slices.Clone(h.Security), security...)
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
