# specout

Generate an OpenAPI 3.1 document from your Go types. Keep your router, your
middleware, and your HTTP handlers.

![specout: OpenAPI from Go types. Keep your HTTP handlers.](release-assets/social/specout-og.png)

> **Experimental:** The public API can change. Feedback from real services is
> welcome.

## Why specout

specout adds API documentation to a Go service that already works. You describe
each route with Go types and field tags, and specout builds the document from
them.

Your handlers do not change. They still receive `http.ResponseWriter` and
`*http.Request`, and they still read, validate, and answer each request. You do
not need comment annotations or a code generation step.

specout only describes your API. It does not decode or validate requests, so
your code must enforce what the tags say.

## Install

specout requires Go 1.27 or later.

```sh
go get github.com/happytoolin/specout@latest
```

## Quick start

This service has one route. Save it as `main.go` in your Go module:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/happytoolin/specout"
)

// GreetRequest describes the request parameters.
type GreetRequest struct {
	Name     string `path:"name" jsonschema:"minLength=1,maxLength=40"`
	Language string `query:"language,omitempty" jsonschema:"enum=en|es,default=en"`
}

// Greeting is the response body.
type Greeting struct {
	Message string `json:"message"`
}

// Problem is the error body.
type Problem struct {
	Error string `json:"error"`
}

// greet is a normal net/http handler. It reads and checks its own input.
func greet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := r.PathValue("name")
	switch r.URL.Query().Get("language") {
	case "", "en":
		json.NewEncoder(w).Encode(Greeting{Message: "Hello, " + name})
	case "es":
		json.NewEncoder(w).Encode(Greeting{Message: "Hola, " + name})
	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Problem{Error: "language must be en or es"})
	}
}

func main() {
	doc := specout.New(specout.Config{Title: "Hello API", Version: "1.0.0"})
	mux := http.NewServeMux()
	routes := specout.Std(doc, mux)

	routes.Get("/hello/{name}", specout.Handler[GreetRequest, Greeting]{
		HandlerFunc: greet,
		Summary:     "Say hello",
		Responses:   []specout.Response{{Status: http.StatusBadRequest, Type: Problem{}}},
	})
	mux.Handle("GET /openapi.json", doc)

	log.Fatal(http.ListenAndServe("localhost:8080", mux))
}
```

Start the service:

```sh
go run .
```

Call the route:

```sh
curl 'http://localhost:8080/hello/Ada?language=es'
```

```json
{"message":"Hola, Ada"}
```

Get the API document:

```sh
curl http://localhost:8080/openapi.json
```

The document includes this operation, and a schema for each of the two response
types:

```json
{
  "summary": "Say hello",
  "operationId": "getHelloName",
  "parameters": [
    {
      "name": "name",
      "in": "path",
      "required": true,
      "schema": { "type": "string", "maxLength": 40, "minLength": 1 }
    },
    {
      "name": "language",
      "in": "query",
      "required": false,
      "schema": { "type": "string", "enum": ["en", "es"], "default": "en" }
    }
  ],
  "responses": {
    "200": {
      "description": "OK",
      "content": {
        "application/json": { "schema": { "$ref": "#/components/schemas/Greeting" } }
      }
    },
    "400": {
      "description": "Bad Request",
      "content": {
        "application/json": { "schema": { "$ref": "#/components/schemas/Problem" } }
      }
    }
  }
}
```

## Compare with other tools

specout documents the API that you already have. Huma and Fuego are frameworks
that also decode and validate requests for you. swag reads comment annotations.

Two features are specific to specout. It finds routes that have no
documentation, and it compares the statuses in your HTTP tests with the document.

| | specout | [Huma](https://huma.rocks/) | [Fuego](https://github.com/go-fuego/fuego) | [swag](https://github.com/swaggo/swag) |
|---|---|---|---|---|
| **Handlers** | | | | |
| Plain `net/http` handlers | Yes | No, it uses its own handler signature | Yes, with `GetStd` | Yes |
| Describe the API with | Go types and tags | Go types and tags | Go types and route options | Comments |
| Build step | None | None | None | `swag init` |
| Decodes and validates requests | No, your handlers do | Yes | Yes | No |
| **OpenAPI** | | | | |
| Document version | 3.1.0 | 3.1.0, and also 3.0.3 | 3.1.0 | 2.0, or 3.1.0 with `--v3.1` on v2 RC |
| Cookie parameters | Yes | Yes | Yes | — |
| Discriminated unions | Yes, from Go types | Yes, with a handwritten schema | — | Yes, with `@Discriminator` on v2 RC |
| Callbacks | With `Raw` | Yes | — | — |
| **Checks** | | | | |
| Find routes with no documentation | Yes | — | — | — |
| Compare test statuses with the document | Yes | — | — | — |

Router support:

| Router | specout | Huma | Fuego |
|---|---|---|---|
| `http.ServeMux` | Yes | Yes | Yes |
| chi v5 | Yes | Yes | — |
| gorilla/mux | Yes | Yes | Yes |
| Echo v5 | Yes | Yes | — |
| Echo v4 | Yes | Yes | Yes |
| Gin | Yes | Yes | Yes |
| Fiber v3 | Yes | Yes | — |
| Fiber v2 | No, Fiber deprecated it | Yes | — |
| httprouter, BunRouter, Flow | — | Yes | — |

swag reads comments, so it works with any router.

A dash means that we did not find the feature in the tool's documentation or
source. We checked Huma v2.40.0, Fuego v0.21.0, and swag v1.16.6 and
v2.0.0-rc6 on 2026-10-30. These tools change, so check their documentation
before you choose.

Choose specout to add documentation and keep your current request handling code.
Choose Huma or Fuego if you want the framework to decode and validate requests.

## Features

| Area | Support |
|---|---|
| Output | OpenAPI 3.1 and JSON Schema 2020-12, as JSON in a stable order. |
| Parameters | Path, query, header, and cookie. |
| Request bodies | JSON, binary files, multipart uploads, and custom media types. |
| Responses | Status codes, headers, media types, shared errors, and responses without a body. |
| Schemas | Nested and recursive types, collections, optional fields, nullable values, enums, formats, value limits, and discriminated unions. |
| Authentication | Bearer tokens, HTTP Basic, API keys, OAuth 2.0, OpenID Connect, and mutual TLS. Your middleware checks the credentials. |
| Route details | Summaries, descriptions, tags, operation IDs, deprecation, examples, and security for each route. |
| Checks | Find routes without documentation, and compare response statuses with your HTTP tests. |

## Describe your API

### Requests

A field tag gives the location of each request value. A field with no location
tag is part of the JSON request body:

```go
type UpdateItemRequest struct {
	ID      int64   `path:"id" jsonschema:"format=int64,description=Item ID"`
	DryRun  bool    `query:"dry_run,omitempty"`
	TraceID string  `header:"X-Trace-ID,omitempty"`
	Session string  `cookie:"session,omitempty"`
	Name    string  `json:"name" jsonschema:"minLength=1"`
	Price   float64 `json:"price" jsonschema:"minimum=0"`
}
```

Here, the first four fields become parameters. The request body has only `name`
and `price`.

| Tag | What it describes |
|---|---|
| `path`, `query`, `header`, `cookie` | The location of a parameter. |
| `json` | The name of a body field. `omitempty` makes the field optional. |
| `jsonschema:"minLength=1,maxLength=40"` | The length limits of a string. |
| `jsonschema:"minimum=1,maximum=3"` | The limits of a number. |
| `jsonschema:"minItems=1,maxItems=3"` | The length limits of an array. |
| `jsonschema:"enum=en\|es"` | The allowed values. |
| `jsonschema:"default=en"` | A default value. Your handler must apply it. |

### Responses

The second type of `specout.Handler` is the `200` response. Add other responses
with `Responses`:

```go
routes.Put("/items/{id}", specout.Handler[UpdateItemRequest, Item]{
	HandlerFunc: updateItem,
	Summary:     "Update an item",
	Responses:   []specout.Response{{Status: http.StatusNotFound, Type: Problem{}}},
})
```

Use `specout.Get[Item]` for a route with no request data. Use
`specout.NoContent` as the response type for a route with no response body.

### The whole API

Only `Title` and `Version` are required in `specout.Config`. Add other fields
when your API needs them:

| Field | Purpose |
|---|---|
| `Description`, `Contact`, `TermsOfService`, `License` | Give general information about the API. |
| `ExternalDocs` | Link to an API guide. |
| `Servers` | List the base URLs of the API. This does not change the server address or the route paths. |
| `Tags` | Describe each group of routes. Use `Handler.Tags` to put a route in a group. |
| `Auth` | Describe authentication. Your middleware must check the credentials. |
| `ErrorType`, `DefaultErrors` | Describe a shared error body and its status codes. Your handlers write these responses. |
| `ClosedSchemas` | Mark object schemas as closed to unknown fields. Your code must reject those fields. |

### Types with custom JSON

specout builds a schema from the Go fields of a type. A type with its own
`MarshalJSON` method can write a different shape, and specout cannot see that
shape. Give the type a `JSONSchema()` method, which comes from
[invopop/jsonschema](https://github.com/invopop/jsonschema):

```go
// Money.MarshalJSON writes a string such as "12.50".
func (Money) JSONSchema() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }
```

Schemas describe the output of `encoding/json`. `encoding/json/v2` writes some
values differently. For example, it writes a `[N]byte` array as a base64 string,
and a nil slice or map as an empty value instead of `null`.

## Use your router

| Router | Function | Package |
|---|---|---|
| `http.ServeMux` | `specout.Std` | `github.com/happytoolin/specout` |
| chi | `specout.Chi` | `github.com/happytoolin/specout` |
| gorilla/mux | `specout.Gorilla` | `github.com/happytoolin/specout` |
| Echo v5 | `specoutecho.New` | `github.com/happytoolin/specout/adapters/specoutecho` |
| Echo v4 | `specoutechov4.New` | `github.com/happytoolin/specout/adapters/specoutechov4` |
| Gin | `specoutgin.New` | `github.com/happytoolin/specout/adapters/specoutgin` |
| Fiber v3 | `specoutfiber.New` | `github.com/happytoolin/specout/adapters/specoutfiber` |

Your build includes Echo, Gin, or Fiber only when you import its adapter.

The Echo, Gin, and Fiber adapters work the same way. This example uses Gin:

```go
doc := specout.New(specout.Config{Title: "Inventory API", Version: "1.0.0"})
engine := gin.New()

routes := specoutgin.New(doc, engine.Group("/v1"))
routes.Get("/items/:id", specout.Handler[GetItemRequest, Item]{
	HandlerFunc: getItem,
	Summary:     "Get an item",
})
engine.GET("/openapi.json", gin.WrapH(doc))

if err := specoutgin.Adopt(doc, engine, specout.Skip("/openapi.json")); err != nil {
	log.Fatal(err)
}
```

The document shows each path in OpenAPI form, so `/items/:id` becomes
`/items/{id}`. Your handler reads the value with `r.PathValue("id")` on every
router.

For another router, use `specout.Bind` to register and document each route. Use
`specout.Document` when the router registers the handler and you only want the
documentation. To request an adapter, open an
[issue](https://github.com/happytoolin/specout/issues).

### Encoded path values

A request for `/users/a%40b` gives one of these values:

| Router | Value of `r.PathValue` | To get `a@b` |
|---|---|---|
| `http.ServeMux`, gorilla/mux, Echo v4, Gin, Fiber | `a@b` | No action. |
| Echo v5 | `a%40b` | Set `UnescapePathParamValues` in `echo.RouterConfig`. |
| chi | `a%40b` | Call `url.PathUnescape` on the value. |

Two router settings keep the value encoded:

- gorilla/mux with `UseEncodedPath`.
- Gin with `UseRawPath` or `UseEscapedPath`, and `UnescapePathValues` cleared.

### Paths

- A path with an empty segment (`//`) or a dot segment (`/.`, `/..`) panics when
  you register it. Clients remove those segments, so the document would show a
  different path.
- The document leaves out catch-all routes. A chi parameter is a catch-all when
  its regex can match a `/` before more text, as in `/files/{name:.+}.json`.
- `http.ServeMux` serves `/a/{$}` for `/a/` only, and `/a/` for every path below
  it. OpenAPI has one path for both, so the build fails if `specout.Std`
  registers both. Register one of them with `specout.Std`, and the other on the
  mux. Give the recorder a skip rule for the second one, as in
  `recorder.New(mux, specout.Skip("/a/"))`.

## Serve or export the document

`Generator` is an `http.Handler`, so you can serve it on any route. Use
`WriteJSON` to write the document to a file or a buffer:

```go
var out bytes.Buffer
if err := doc.WriteJSON(&out); err != nil {
	return err
}
```

Register all routes first. The first `ServeHTTP` or `WriteJSON` call builds the
document, and you cannot add routes after a successful build.

### When something is wrong

| Problem | Result |
|---|---|
| Bad `Config` | `specout.New` panics. |
| Bad route data or bad path | The registration panics, and the router does not get the route. |
| Failed build, with `WriteJSON` | `WriteJSON` returns the error. If the build panics, `WriteJSON` panics. |
| Failed build, with `ServeHTTP` | `ServeHTTP` returns `500` without the error text. It logs the error once, and also the stack after a panic. |

A few path checks need the full path, and the router knows it only after it adds
the route. In these cases, the router has the route when the panic occurs:

- A path that a `specout.Bind` mount function returns.
- A gorilla/mux subrouter prefix or host.
- An empty gorilla/mux path.

## Check your documentation

### Find routes without documentation

`Adopt` returns an error that names each route with no documentation. It works
with chi, gorilla/mux, Echo, Gin, and Fiber. Use `RequireDocumented` to make the
same check in a test:

```go
func TestRoutesAreDocumented(t *testing.T) {
	specoutgin.RequireDocumented(t, doc, engine, specout.Skip("/openapi.json"))
}
```

With chi, call `Adopt` on the outermost router after you register the routes.
This also gives specout the full paths of route groups and mounted routers. If
chi serves the document, the build fails when an adopted router is mounted under
a prefix that no server URL declares.

`http.ServeMux` cannot list its routes. Register each route that needs
documentation with `specout.Std`.

### Compare response statuses with your tests

The optional `recorder` package records the response statuses of your HTTP tests.
`recorder.Verify` then checks that each declared status occurred, and that the
document declares each status that occurred. It does not check response bodies.

```go
func TestStatuses(t *testing.T) {
	rec := recorder.New(mux)
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hello/Ada", nil))
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hello/Ada?language=fr", nil))
	recorder.Verify(t, doc, rec)
}
```

With Echo, Gin, and Fiber, use `recorder.New(nil)` and install the adapter's
`Record` middleware.

A handler that hijacks the connection writes its own response. If the handler
wrote no status and the request asks to switch protocols, as in a WebSocket
handshake, the recorder records `101`. It records nothing for any other hijack
that wrote no status.

## Learn more

- [Runnable examples](examples/README.md)
- [Sample API document](examples/onboarding/openapi.json)
- [Types and responses](skills/specout/references/types-and-responses.md)
- [Routers and checks](skills/specout/references/routers-and-verification.md)
- [Configuration and authentication](skills/specout/references/configuration.md)
- [OpenAPI 3.1 field coverage](skills/specout/references/openapi-coverage.md)

Coding agents can use the optional [specout skill](skills/specout/SKILL.md):

```sh
npx skills add happytoolin/specout --skill specout
```

## Contribute

Feedback and contributions are welcome, and so are new router adapters. Read the
[code guide](CONTRIBUTING.md) for the file layout.

```sh
just check    # Run all CI and release checks.
just lint     # Check Go lint rules and formatting.
just nilaway  # Find code that can panic on nil.
just demo     # Run the onboarding example.
```

`just check` requires Go, Python 3, and Node.js with npm.

## License

[Apache 2.0](LICENSE)
