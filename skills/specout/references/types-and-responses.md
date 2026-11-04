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

A style must fit the field type. `deepObject` needs a struct or a map, and
`explode=false` panics with it. `spaceDelimited` and `pipeDelimited` need a
slice, an array, a struct or a map.

The same tag sets the other parameter keywords. `allowEmptyValue` and
`allowReserved` work on query parameters only:

```go
Old    string `query:"old,omitempty" jsonschema:"deprecated"`
Search string `query:"q,omitempty" jsonschema:"allowEmptyValue=true"`
Target string `query:"target,omitempty" jsonschema:"allowReserved=true"`
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

For an enum that several fields or parameters share, give a named string type a
`JSONSchema` method that returns `specout.Enum`. Each use of the type then has
one list. `specout.Schema` is the schema type of the jsonschema package, so no
other import is needed:

```go
type state string

func (state) JSONSchema() *specout.Schema { return specout.Enum("draft", "active", "archived") }
```

Nested structs, recursive types, slices, arrays, maps, named scalar types, and
generic instantiations become reusable components. Use
`jsonschema_description` or `jsonschema_extras` only when the underlying
jsonschema package needs them.

specout panics on a tag value that would make the schema break JSON Schema
2020-12:

- A `default` or `example` that the field schema rejects, such as
  `enum=en|es,default=de`. A list default, such as `default=1` on `[]int`,
  becomes `[1]`.
- A `pattern` that is not a regular expression, `multipleOf=0`, an `anchor` that
  is not a name, or a repeated enum value.
- A `jsonschema_extras` key that the `jsonschema` tag checks, such as `required`
  or `minimum`. Write it in the `jsonschema` tag.
- `nullable` in `jsonschema_extras`. It is an OpenAPI 3.0 keyword. Use a pointer
  field.
- A `oneof_ref` or `anyof_ref` to a type that no route uses, or to a bare name.

A header parameter named `Accept`, `Content-Type` or `Authorization` panics.
OpenAPI ignores these parameters. Use `ContentTypes` and `Config.Auth` for them.

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

A `[]specout.File` body panics, because a JSON body cannot carry files. Put the
list in a struct field, such as `Files []specout.File`. A struct with a file
field panics when a `ContentTypes` entry is not `multipart/*`. A `TRACE` route
with a body panics.

Publish one body schema under custom media types when the wire format supports
them:

```go
h := specout.Handler[importRequest, importResult]{
	HandlerFunc:         importItems,
	Request: specout.Request{ContentTypes: []string{"application/json", "application/xml"}},
}
```

Use `Request.Encoding` to describe the fields of a form or multipart body. Each key
is a field name on the form, so a `form` tag replaces the JSON name. Other
multipart types, such as `multipart/mixed`, use the JSON names. A key that is not
a field panics, and so does a body that is not a form:

```go
h := specout.Handler[uploadRequest, uploadResult]{
	HandlerFunc: upload,
	Request: specout.Request{Encoding: map[string]specout.Encoding{
		"file": {ContentType: "image/png"},
		"tags": {Style: "pipeDelimited", Explode: new(false)},
	}},
}
```

## Response defaults

- A non-empty response type declares `200` with an `application/json` body.
- `specout.NoContent` or another empty struct declares `204`.
- A declared success response replaces the inferred `200` or `204`. A success
  response is a `2xx`, a redirect (a `3xx` other than `304`), or a `101`. Any
  other declared response keeps it, so a `404` on a `specout.Delete` keeps the
  `204`, and a `304` keeps the `200`.
- Explicit responses add or replace entries with the same status or key.
- A response with no `Type` gets the body of its status. A `2xx` has the
  handler response type. A `4xx`, `5xx`, or `default` response has
  `Config.ErrorType`, or no body when `ErrorType` is not set. Any other status
  has no body. A webhook or callback never gets `ErrorType`.
- `Type: specout.NoContent{}` declares a response with no body.
- Status `1xx`, `204`, `205`, and `304` never have a body.
- Status `0` or `Key: "default"` emits the OpenAPI default response.
- `Key: "4XX"` emits a response range. Valid ranges are `1XX` through `5XX`.
- `Omit: true` removes the response at that status or key: the inferred `200`
  or `204`, or a status from `DefaultErrors`.
- A route with `Public: true` does not get `401` or `403` from `DefaultErrors`.

## Statuses, error types, and headers

This create endpoint returns only `201` on success. The `201` replaces the
inferred `200`. The `422` has its own error type, and the `201` documents
`Location`:

```go
type validationError struct {
	Problems []fieldProblem `json:"problems"`
}

h := specout.Handler[createRequest, item]{
	HandlerFunc: createItem,
	Responses: []specout.Response{
		{
			Status:  http.StatusCreated,
			Headers: []specout.Header{{Name: "Location"}},
		},
		{Status: http.StatusUnprocessableEntity, Type: validationError{}},
	},
}
```

When the endpoint can return both `200` and `201`, declare both. A `2xx` with
no `Type` has the handler response type.

Use `Header.Type` when a response header is not a string:

```go
specout.Header{Name: "X-RateLimit-Remaining", Type: int64(0)}
```

## Examples

Add one example with `Example`, or several named examples with `Examples`. Set
one of them. specout encodes the values with `encoding/json`. A response needs a
body to show an example, and a HEAD response drops its examples:

```go
h := specout.Handler[createRequest, item]{
	HandlerFunc: createItem,
	Request:     specout.Request{Description: "The item to create.", Example: createRequest{Name: "Lamp"}},
	Responses: []specout.Response{{
		Status:      http.StatusOK,
		Description: "The new item.",
		Examples: map[string]specout.Example{
			"lamp": {Summary: "A lamp", Value: item{ID: 1, Name: "Lamp"}},
			"file": {ExternalValue: "https://example.com/item.json"},
		},
	}},
}
```

An `Example` sets one of `Value` and `ExternalValue`. For a parameter, use the
schema example: `jsonschema:"example=42"`.

A request example can be a `Req` value. Each media type then gets the example in
the shape of its body. specout drops the parameter fields and the `File` fields,
and a form media type uses the `form` names.

## Binary and multiple response media types

`specout.File` is a binary body. It is `application/octet-stream` by default. Use
it as `Res`, or as `Response.Type` when the route has another body too. Set
`ContentTypes` for its media types:

```go
download := specout.Handler[downloadRequest, specout.File]{
	HandlerFunc: downloadReport,
	Responses: []specout.Response{{
		Status:       http.StatusOK,
		ContentTypes: []string{"application/pdf"},
	}},
}
```

Use `ContentTypes` when one typed response is available in several formats:

```go
specout.Response{
	Status:       http.StatusOK,
	ContentTypes: []string{"application/json", "application/xml"},
}
```

Use `Links` to tell a client how to use a response in a next request. Set
`OperationID` or `OperationRef`. The build fails when no operation has the
`OperationID`:

```go
specout.Response{
	Status: http.StatusOK,
	Links: map[string]specout.Link{
		"status": {
			OperationID: "getJob",
			Parameters:  map[string]string{"id": "$response.body#/id"},
		},
	},
}
```

Use `Response.Raw` for `x-` extensions, or to replace a whole field that specout
writes. `Raw` panics when the response also sets the typed form of that field:
`Description`, `Headers`, or for `content` the `ContentTypes`, `Example` and
`Examples` fields.

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
