package specout

// Config is the API-level configuration for a Generator. The zero value is a
// valid, empty spec; Title and Version are required for a usable document.
type Config struct {
	Title         string
	Version       string
	Description   string
	Servers       []Server
	Auth          []AuthScheme
	ExternalDocs  *ExternalDocs
	Tags          []Tag
	ErrorType     any
	DefaultErrors []int
	ClosedSchemas bool
	JSONDialect   Dialect
}

// Server is an OpenAPI server entry.
type Server struct {
	URL         string
	Description string
}

// Tag is a named tag with a description, for the document's top-level tag list.
type Tag struct {
	Name        string
	Description string
}

// ExternalDocs links to external documentation.
type ExternalDocs struct {
	URL         string
	Description string
}

// AuthScheme is a security scheme declaration, doc-only: it emits the
// securitySchemes component and one top-level security alternative per
// scheme (any one satisfies). Handlers opt out per route with Public.
type AuthScheme struct {
	Name string // scheme name in securitySchemes (e.g. "bearerAuth")
	Type string // httpBearer, apiKey, openIdConnect
	Key  string // apiKey parameter name
	In   string // apiKey parameter location: header, query, cookie
	URL  string // openIdConnect well-known URL
}

var Bearer = AuthScheme{Name: "bearerAuth", Type: "httpBearer"}

// APIKey declares an API-key auth in a header, query param, or cookie.
func APIKey(scheme, key, in string) AuthScheme {
	return AuthScheme{Name: scheme, Type: "apiKey", Key: key, In: in}
}

const (
	InHeader = "header"
	InQuery  = "query"
	InCookie = "cookie"
)

// Dialect selects how struct tags are interpreted for schema generation.
type Dialect int

const (
	// JSONv2 is the default: encoding/json/v2 semantics, with pointer +
	// (omitzero)/omitempty as optional.
	JSONv2 Dialect = iota
	// JSONv1 is the legacy dialect for types still carrying v1 tags.
	JSONv1
)
