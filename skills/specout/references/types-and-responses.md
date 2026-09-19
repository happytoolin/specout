# Types and responses

Use this reference to model the HTTP wire contract with Go types and tags.

## Common handler shapes

| HTTP shape | Handler type |
|---|---|
| No request data, JSON response | `specout.Handler[struct{}, response]` or `specout.Get[response]` |
| Parameters and JSON response | `specout.Handler[request, response]` |
| JSON body and no response body | `specout.Handler[request, specout.NoContent]` |
| No request data and no response body | `specout.Delete` |
| JSON array body | `specout.Handler[[]item, response]` |
| Raw file body | `specout.Handler[specout.File, response]` |

The generic types describe documentation only. The embedded
`http.HandlerFunc` still reads the request and writes the response.

## Parameters and a JSON body in one type

Use location tags for parameters. Fields without a location tag remain in the
JSON request body.

```go
type updateItemRequest struct {
	ID      int64   `path:"id" jsonschema:"format=int64,description=Item ID"`
	DryRun  bool    `query:"dry_run,omitempty"`
	TraceID string  `header:"X-Trace-ID,omitempty"`
	Session string  `cookie:"session,omitempty"`
	Name    string  `json:"name" jsonschema:"minLength=1"`
	Price   float64 `json:"price" jsonschema:"minimum=0"`
}

routes.Put("/items/{id}", specout.Handler[updateItemRequest, item]{
	HandlerFunc: updateItem,
	Summary:     "Update an item",
})
```

The emitted request body contains only `name` and `price`. The four tagged
fields become OpenAPI parameters. A `path` field must match a path placeholder.
When no matching field exists, the path parameter is documented as a string.

Supported parameter tags are `path`, `query`, `header`, and `cookie`.
`json:"-"` does not hide a parameter. Use a location value of `-`, such as
`query:"-"`, when a field must stay in the body instead.

Parameter `style` and `explode` use the `jsonschema` tag:

```go
Filter map[string]string `query:"filter" jsonschema:"style=deepObject,explode=true"`
```

## Required, optional, and nullable values

- A path parameter is always required.
- A parameter is optional when its location tag or JSON tag contains
  `omitempty` or `omitzero`, or its `jsonschema` tag supplies a default.
- A JSON property is optional when its JSON tag contains `omitempty` or
  `omitzero`.
- A pointer makes the schema nullable. Optional and nullable are separate.
- An empty request struct emits no request body.
- A request type that contains only parameters emits no request body.

```go
type filter struct {
	Limit  int     `query:"limit" jsonschema:"minimum=1,maximum=100,default=20"`
	Sort   string  `query:"sort,omitempty" jsonschema:"enum=created|updated"`
	Cursor *string `query:"cursor,omitempty"`
	Note   *string `json:"note,omitempty"`
}
```

`Cursor` and `Note` are both optional and nullable. `Limit` is optional because
it has a default.

## JSON Schema tags and Go types

specout uses github.com/invopop/jsonschema and adds OpenAPI-oriented fixes.
Common tags include:

```go
type item struct {
	ID        int64      `json:"id" jsonschema:"readonly,format=int64"`
	Name      string     `json:"name" jsonschema:"minLength=1,maxLength=120"`
	State     string     `json:"state" jsonschema:"enum=draft|active|archived,default=draft"`
	SKU       string     `json:"sku" jsonschema:"pattern=^[A-Z0-9-]+$"`
	Email     string     `json:"email" jsonschema:"format=email"`
	CreatedAt time.Time  `json:"createdAt" jsonschema:"readonly"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}
```

Nested structs, recursive types, slices, arrays, maps, named scalar types, and
generic instantiations become reusable components. Use
`jsonschema_description` or `jsonschema_extras` only when the underlying
jsonschema package needs them.

If two Go types produce the same component name, assign one explicitly before
the document builds:

```go
doc.SchemaName[internal.Contact]("InternalContact")
```

## Request body media types and files

A normal body uses `application/json`. A bare `specout.File` uses
`application/octet-stream`:

```go
upload := specout.Handler[specout.File, specout.NoContent]{
	HandlerFunc: uploadArchive,
}
```

A struct with a `specout.File` field uses `multipart/form-data`:

```go
type uploadRequest struct {
	File        specout.File `json:"file"`
	Description string       `json:"description,omitempty"`
}
```

The marker only documents the payload. The handler still reads the body or
multipart form.

Publish one body schema under custom media types when the wire format supports
them:

```go
h := specout.Handler[importRequest, importResult]{
	HandlerFunc:         importItems,
	RequestContentTypes: []string{"application/json", "application/xml"},
}
```

## Response defaults

- A non-empty response type declares `200` with an `application/json` body.
- `specout.NoContent` or another empty struct declares `204` when there is no
  explicit response.
- Explicit responses add or replace entries with the same status or key.
- Status `204` and `304` have no body unless `Type` is set explicitly.
- Status `0` or `Key: "default"` emits the OpenAPI default response.
- `Key: "4XX"` emits a response range. Valid ranges are `1XX` through `5XX`.

## Statuses, error types, and headers

This create endpoint returns only `201` on success. It removes the inferred
`200`, declares a typed validation error, and documents `Location`:

```go
type validationError struct {
	Problems []fieldProblem `json:"problems"`
}

h := specout.Handler[createRequest, item]{
	HandlerFunc: createItem,
	Responses: []specout.Response{
		{Status: http.StatusOK, Omit: true},
		{
			Status:  http.StatusCreated,
			Headers: []specout.Header{{Name: "Location"}},
		},
		{Status: http.StatusUnprocessableEntity, Type: validationError{}},
	},
}
```

When the endpoint can return both `200` and `201`, do not omit the inferred
`200`. A response with no `Type` inherits the handler response type.

Use `Header.Type` when a response header is not a string:

```go
specout.Header{Name: "X-RateLimit-Remaining", Type: int64(0)}
```

## Binary and multiple response media types

Use `ContentType` for a binary response:

```go
download := specout.Handler[downloadRequest, struct{}]{
	HandlerFunc: downloadReport,
	Responses: []specout.Response{{
		Status:      http.StatusOK,
		ContentType: "application/pdf",
	}},
}
```

`ContentType: "binary"` is shorthand for `application/octet-stream`.

Use `ContentTypes` when one typed response is available in several formats:

```go
specout.Response{
	Status:       http.StatusOK,
	ContentTypes: []string{"application/json", "application/xml"},
}
```

Use `Response.Raw` only for response fields that the typed API does not model:

```go
specout.Response{
	Key: "4XX",
	Type: problem{},
	Raw:  map[string]any{"description": "Client error"},
}
```

If a range must also create a recorder coverage expectation, set one concrete
`Status` beside the range key, such as `{Status: 404, Key: "4XX"}`.

## Discriminated unions

Register each variant before the document builds. The discriminator uses an
enum field with the exact `description=Discriminator` marker. The `data` field
uses `oneof_type` with the same variant names.

```go
type emailWebhook struct {
	Address string `json:"address" jsonschema:"format=email"`
}

type slackWebhook struct {
	Channel string `json:"channel"`
}

type webhook struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}

doc.Register[emailWebhook]("email")
doc.Register[slackWebhook]("slack")
```

The resulting schema constrains each discriminator value and payload type
together. A missing registered variant fails the build.
