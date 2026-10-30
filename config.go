package specout

// Config is the API-level configuration for a Generator. The zero value is a
// valid, empty spec; Title and Version are required for a usable document.
// New panics on a Config that would make the document invalid, such as a
// status with no body (1xx, 204, 205, 304) in DefaultErrors.
type Config struct {
	Title          string
	Version        string
	Summary        string
	Description    string
	TermsOfService string
	Contact        *Contact
	License        *License
	Servers        []Server
	Auth           []AuthScheme
	ExternalDocs   *ExternalDocs
	Tags           []Tag
	ErrorType      any
	DefaultErrors  []int
	ClosedSchemas  bool
	// Raw splices keys into the document root: x- extensions, webhooks, or
	// jsonSchemaDialect. Any other key panics in New.
	Raw map[string]any
}

// Contact is the info.contact object: who owns the API.
type Contact struct {
	Name  string
	URL   string
	Email string
}

// License is the info.license object. OpenAPI 3.1 takes either a URL or an
// SPDX Identifier such as "Apache-2.0", not both.
type License struct {
	Name       string
	URL        string
	Identifier string
}

// Server is an OpenAPI server entry.
type Server struct {
	URL         string
	Description string
	// Variables fills the {name} placeholders of URL.
	Variables map[string]ServerVariable
}

// ServerVariable is one {name} placeholder of a server URL. Default is
// required. When Enum is set, it must hold Default.
type ServerVariable struct {
	Default     string
	Enum        []string
	Description string
}

// Tag is a named tag with a description, for the document's top-level tag list.
type Tag struct {
	Name         string
	Description  string
	ExternalDocs *ExternalDocs
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
	Name         string                // scheme name in securitySchemes (e.g. "bearerAuth")
	Type         string                // httpBearer, http, apiKey, oauth2, openIdConnect, mutualTLS
	Scheme       string                // http: the authorization scheme, such as "basic" or "digest"
	BearerFormat string                // bearer: a hint for the token format, such as "JWT"
	Description  string                // what the reader needs to know to authenticate
	Key          string                // apiKey parameter name
	In           string                // apiKey parameter location: header, query, cookie
	URL          string                // openIdConnect well-known URL
	Flows        map[string]OAuth2Flow // oauth2 flows, keyed by flow name
}

// OAuth2Flow is one entry of an oauth2 scheme's flows object. The flow name
// (implicit, password, clientCredentials, authorizationCode) selects which
// URLs the OpenAPI validator requires; securitySchemeObj enforces that.
type OAuth2Flow struct {
	AuthorizationURL string
	TokenURL         string
	RefreshURL       string
	Scopes           map[string]string
}

// Bearer is the standard Authorization: Bearer scheme, ready for Config.Auth.
var Bearer = AuthScheme{Name: "bearerAuth", Type: typeHTTPBearer}

// Basic is the standard HTTP Basic scheme, ready for Config.Auth.
var Basic = AuthScheme{Name: "basicAuth", Type: "http", Scheme: "basic"}

// OAuth2 declares an oauth2 scheme with the publisher's flows and scopes.
func OAuth2(scheme string, flows map[string]OAuth2Flow) AuthScheme {
	return AuthScheme{Name: scheme, Type: "oauth2", Flows: flows}
}

// APIKey declares an API-key auth in a header, query param, or cookie.
func APIKey(scheme, key, in string) AuthScheme {
	return AuthScheme{Name: scheme, Type: "apiKey", Key: key, In: in}
}

// APIKey.In values: where the key travels.
const (
	InHeader = "header"
	InQuery  = "query"
	InCookie = "cookie"
)
