package specout

// Config is the API-level configuration for a Generator. The zero value is a
// valid, empty spec; Title and Version are required for a usable document.
type Config struct {
	Title         string
	Version       string
	Description   string
	Servers       []Server
	Auth          AuthScheme
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

// AuthScheme is a security scheme declaration. Phase 1 ships Bearer as the
// only named value; more schemes arrive with later phases.
type AuthScheme string

const Bearer AuthScheme = "bearer"

// Dialect selects how struct tags are interpreted for schema generation.
type Dialect int

const (
	// JSONv2 is the default: encoding/json/v2 semantics, with pointer +
	// (omitzero)/omitempty as optional.
	JSONv2 Dialect = iota
	// JSONv1 is the legacy dialect for types still carrying v1 tags.
	JSONv1
)
