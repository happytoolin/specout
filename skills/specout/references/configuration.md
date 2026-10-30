# Configuration

Use this reference for `specout.Config`, authentication, operation metadata,
and generator-level schema settings.

## Complete configuration shape

`Title` and `Version` are required, and they are all that a normal document needs. Add other fields
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
| `Summary` | Adds a short summary of the API. |
| `Description` | Adds the API description. |
| `TermsOfService` | Adds the terms URL. |
| `Contact` | Adds contact name, URL, and email when set. |
| `License` | Adds license name, and a URL or an SPDX `Identifier`. |
| `Servers` | Publishes the ordered server list. `Variables` fills `{name}` placeholders in a URL. |
| `Tags` | Declares top-level tags and optional tag documentation. |
| `ExternalDocs` | Links the whole API to external documentation. Its URL is required. |
| `ErrorType` | Sets the schema used by shared error responses. |
| `DefaultErrors` | Adds shared status codes to each operation when `ErrorType` is set. |
| `ClosedSchemas` | Emits `additionalProperties: false` for object schemas while preserving map values. |
| `Auth` | Publishes documentation-only security schemes and top-level requirements. |
| `JSONSchemaDialect` | Sets the default JSON Schema dialect of the document. Most APIs do not need it. |
| `Raw` | Adds `x-` extensions to the document root. Any other key panics. |

`ErrorType` and `DefaultErrors` form one feature. A list of default codes without
an error type panics. Default errors are allowed by recorder
verification but are not required to be produced on every route.

`ClosedSchemas` is useful when clients must reject unknown object fields. Do not
enable it only for style because it changes the published contract.

## Authentication schemes

Authentication settings only document security. They do not install middleware
or enforce credentials.

Bearer and Basic authentication use built-in declarations:

```go
Auth: []specout.AuthScheme{specout.Bearer, specout.Basic}
```

Add details with a direct `AuthScheme` value. Use `Type: "http"` with `Scheme`
for another HTTP scheme. Use `specout.MutualTLS` for client certificates:

```go
Auth: []specout.AuthScheme{
	{Name: "jwt", Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "A signed token."},
	{Name: "digest", Type: "http", Scheme: "digest"},
	specout.MutualTLS("mtls"),
}
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

Use `specout.OpenIDConnect` for OpenID Connect:

```go
Auth: []specout.AuthScheme{
	specout.OpenIDConnect("oidc", "https://id.example.com/.well-known/openid-configuration"),
}
```

Several entries in `Auth` are OpenAPI alternatives. A client can use any one
scheme. They do not mean that every scheme is required together.

Mark a route that does not inherit global authentication as public:

```go
h := specout.Get[health]{HandlerFunc: healthcheck}.WithPublic()
```

Give one route its own requirement with `Security`. It replaces the global
requirement. Each entry names a scheme from `Auth`, and any one entry satisfies
the requirement. A route cannot set both `Public` and `Security`:

```go
oauth := specout.OAuth2("oauth", flows)

h := specout.Get[report]{HandlerFunc: getReport}.WithSecurity(
	oauth.Require("reports:read"),
)
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
- `Security` for an authentication requirement on this route.
- `Request` for the request body: `ContentTypes`, `Description`, `Optional`,
  `Example`, `Examples`, and `Encoding`.
- `Responses` for status-specific contracts.
- `Servers` when this operation has a different base URL.
- `Callbacks` for the requests that your API sends because of this operation.
- `Raw` for `x-` extensions, or to replace a whole field that specout writes.

specout derives an operation ID from the method and path when `OperationID` is
empty. Set it when a stable public name is required or two derived IDs collide.
Duplicate IDs fail the build.

specout derives `GET /jobs/{id}` as `getJobsId`. Set `OperationID` on a route that
a `Response.Links` entry names, so the name stays stable.

Use `Raw` only when the typed fields cannot express the requirement:

```go
h.Raw = map[string]any{
	"x-rate-limit-tier": "standard",
}
```

Raw values replace fields with the same key. Keep security-sensitive and
required OpenAPI fields in the typed configuration when possible. `Raw` panics
on `callbacks`, `servers`, `links`, `webhooks`, and `jsonSchemaDialect`. The
message names the typed field to use.

## Webhooks and callbacks

A webhook or callback is a request that the API sends. Describe it with a
`Handler` that has no `HandlerFunc`. `Req` is what the API sends, and `Res` is
what the receiver answers. `specout.Event[T]` is short for
`specout.Handler[T, specout.NoContent]`.

```go
doc.Webhook(http.MethodPost, "orderCreated", specout.Event[OrderCreated]{
	Summary: "An order was created",
})

jobDone := specout.NewCallback(
	"jobDone", http.MethodPost, "{$request.body#/callbackUrl}",
	specout.Event[JobResult]{Summary: "The job finished"},
)
routes.Post("/jobs", specout.Handler[StartJob, Job]{
	HandlerFunc: startJob,
	Callbacks:   []specout.Callback{jobDone},
})
```

- `header`, `query`, and `cookie` tags on `Req` work, for example a signature
  header. A `path` tag panics, because the request has no path template.
- Use `Responses` with `Key: "2XX"` when the receiver can answer any success
  status.
- The route checks and the recorder ignore these requests.
- `DefaultErrors` does not apply to them.
- They emit `security: []` when the document has `Auth`, unless `Security` names
  a scheme.
- They get no derived operation ID. An `OperationID` that you set must be unique
  in the document, so do not set one on a `Callback` that several routes share.
