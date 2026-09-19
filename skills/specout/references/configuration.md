# Configuration

Use this reference for `specout.Config`, authentication, operation metadata,
and generator-level schema settings.

## Complete configuration shape

Only `Title` and `Version` are needed for a normal document. Add other fields
only when they describe the real API.

```go
type problem struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
}

doc := specout.New(specout.Config{
	Title:          "Inventory API",
	Version:        "1.0.0",
	Description:    "Inventory and stock operations.",
	TermsOfService: "https://example.com/terms",
	Contact: &specout.Contact{
		Name:  "API team",
		URL:   "https://example.com/support",
		Email: "api@example.com",
	},
	License: &specout.License{
		Name: "MIT",
		URL:  "https://opensource.org/license/mit",
	},
	Servers: []specout.Server{
		{URL: "https://api.example.com", Description: "Production"},
	},
	Tags: []specout.Tag{
		{Name: "inventory", Description: "Inventory operations"},
	},
	ExternalDocs: &specout.ExternalDocs{
		URL:         "https://docs.example.com",
		Description: "API guide",
	},
	ErrorType:     problem{},
	DefaultErrors: []int{400, 401, 404, 500},
	ClosedSchemas: true,
	Auth: []specout.AuthScheme{
		specout.Bearer,
		specout.APIKey("apiKey", "X-API-Key", specout.InHeader),
	},
})
```

## Config fields

| Field | Effect |
|---|---|
| `Title`, `Version` | Populate the required OpenAPI `info` fields. |
| `Description` | Adds the API description. |
| `TermsOfService` | Adds the terms URL. |
| `Contact` | Adds contact name, URL, and email when set. |
| `License` | Adds license name and URL. |
| `Servers` | Publishes the ordered server list. |
| `Tags` | Declares top-level tags and optional tag documentation. |
| `ExternalDocs` | Links the whole API to external documentation. Its URL is required. |
| `ErrorType` | Sets the schema used by shared error responses. |
| `DefaultErrors` | Adds shared status codes to each operation when `ErrorType` is set. |
| `ClosedSchemas` | Emits `additionalProperties: false` for object schemas while preserving map values. |
| `Auth` | Publishes documentation-only security schemes and top-level requirements. |

`ErrorType` and `DefaultErrors` form one feature. A list of default codes without
an error type emits no shared responses. Default errors are allowed by recorder
verification but are not required to be produced on every route.

`ClosedSchemas` is useful when clients must reject unknown object fields. Do not
enable it only for style because it changes the published contract.

## Authentication schemes

Authentication settings only document security. They do not install middleware
or enforce credentials.

Bearer authentication uses the built-in declaration:

```go
Auth: []specout.AuthScheme{specout.Bearer}
```

API keys can use a header, query parameter, or cookie:

```go
Auth: []specout.AuthScheme{
	specout.APIKey("headerKey", "X-API-Key", specout.InHeader),
	specout.APIKey("queryKey", "api_key", specout.InQuery),
	specout.APIKey("cookieKey", "session", specout.InCookie),
}
```

OAuth 2.0 supports `implicit`, `password`, `clientCredentials`, and
`authorizationCode` flows:

```go
Auth: []specout.AuthScheme{
	specout.OAuth2("oauth", map[string]specout.OAuth2Flow{
		"authorizationCode": {
			AuthorizationURL: "https://id.example.com/authorize",
			TokenURL:         "https://id.example.com/token",
			RefreshURL:       "https://id.example.com/refresh",
			Scopes: map[string]string{
				"inventory:read":  "Read inventory",
				"inventory:write": "Change inventory",
			},
		},
	}),
}
```

`implicit` needs `AuthorizationURL`. `password` and `clientCredentials` need
`TokenURL`. `authorizationCode` needs both URLs.

OpenID Connect uses a direct `AuthScheme` value:

```go
Auth: []specout.AuthScheme{{
	Name: "oidc",
	Type: "openIdConnect",
	URL:  "https://id.example.com/.well-known/openid-configuration",
}}
```

Several entries in `Auth` are OpenAPI alternatives. A client can use any one
scheme. They do not mean that every scheme is required together.

Mark a route that does not inherit global authentication as public:

```go
h := specout.Get[health]{HandlerFunc: healthcheck}.WithPublic()
```

## Operation metadata

`Handler` fields and their `With*` methods are equivalent. Use the form that is
already common in the project.

```go
h := specout.Handler[createRequest, item]{HandlerFunc: createItem}.
	WithSummary("Create an item").
	WithDescription("Creates one inventory item.").
	WithOperationID("createInventoryItem").
	WithTags("inventory").
	WithExternalDocs(&specout.ExternalDocs{
		URL: "https://docs.example.com/inventory/create",
	}).
	WithDeprecated()
```

Available operation metadata is:

- `Summary`, `Description`, `OperationID`, `Tags`, and `ExternalDocs`.
- `Deprecated` for an obsolete operation.
- `Public` to emit `security: []` when global auth exists.
- `RequestContentTypes` for one body schema under custom media types.
- `Responses` for status-specific contracts.
- `Raw` for unsupported OpenAPI fields and `x-` extensions.

specout derives an operation ID from the method and path when `OperationID` is
empty. Set it when a stable public name is required or two derived IDs collide.
Duplicate IDs fail the build.

Use `Raw` only when the typed fields cannot express the requirement:

```go
h.Raw = map[string]any{
	"x-rate-limit-tier": "standard",
}
```

Raw values replace fields with the same key. Keep security-sensitive and
required OpenAPI fields in the typed configuration when possible.
