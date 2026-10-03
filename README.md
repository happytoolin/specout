# specout

We built specout to add API docs without replacing your router or changing the
request lifecycle.

![specout: OpenAPI from Go types. Keep your HTTP handlers.](release-assets/social/specout-og.png)

Generate OpenAPI from Go types. Keep your existing HTTP handlers.

specout supports `net/http`, chi, and gorilla/mux. You can keep your router and
middleware.

> **Experimental**
>
> specout is experimental. The public API may change.
> Feedback and reports from real services are welcome.

## How it works

Define request and response types. Add field tags to describe parameters and
bodies. Attach the types to your handler with `specout.Handler[Request, Response]`.

Your handler still gets `http.ResponseWriter` and `*http.Request`. It reads the
request. It validates input. It writes the response.

Tags describe the API contract. Your code must enforce it. specout does not
decode or validate requests. You do not need comment annotations or generated
handlers.

## Why specout?

Use specout when you want API docs for an existing Go service. Keep your router,
middleware, and `http.HandlerFunc` handlers. Your code keeps control of request
parsing, validation, authentication, and response writing.

Each tool has a different approach:

- [Huma](https://huma.rocks/features/operations/) uses typed operation handlers.
  It provides request parsing, validation, and response encoding. It supports
  [several routers](https://huma.rocks/features/bring-your-own-router/).
- [Fuego](https://github.com/go-fuego/fuego#features) is a web framework with
  OpenAPI generation, parsing, validation, and response encoding. It also
  supports standard HTTP handlers.
- [Swaggo's swag](https://github.com/swaggo/swag#getting-started) builds API docs
  from comment annotations. Its CLI generates the documentation files.

specout uses Go types and field tags to describe your API. You do not need
comment annotations or a code generation step. Choose it when you want to add
docs while keeping your current request handling code.

## Supported features

Request and response types describe the API contract. Your code must implement
that contract.

| Feature | Support |
|---|---|
| Document format | OpenAPI 3.1 and JSON Schema 2020-12. |
| Router adapters | `http.ServeMux`, chi, and gorilla/mux. |
| Other routers | Use `specout.Document` for metadata. Register handlers with your router. |
| Request parameters | Path, query, header, and cookie parameters. |
| Request bodies | JSON, binary files, multipart uploads, and custom media types. |
| Response contracts | Status codes, headers, media types, shared errors, and responses without a body. |
| Go type schemas | Nested and recursive types, collections, optional fields, and nullable values. |
| Schema rules | Enums, formats, value limits, and registered discriminated unions. |
| Authentication docs | Bearer tokens, API keys, OAuth 2.0, and OpenID Connect. Your middleware checks credentials. |
| Route metadata | Summaries, descriptions, tags, operation IDs, and deprecation. |
| Document output | Serve JSON over HTTP or export it with `WriteJSON`. The output has a stable order. |
| Route documentation checks | chi and gorilla/mux can report routes that have no docs. |
| Response status checks | The optional `recorder` package checks statuses from HTTP tests with supported routers. It does not check response bodies. |
| Automatic parsing and validation | Not provided. Your handlers or middleware do this. |
| Echo and Fiber adapters | Not available yet. See [future router support](#future-router-support). |

## Install

Use **Go 1.27 or later**.

```sh
go get github.com/happytoolin/specout@v0.0.2
```

## Example

This example has three routes:

| Route | Purpose |
|---|---|
| `GET /hello/{name}` | Read a path parameter and an optional query parameter. |
| `POST /greetings` | Read a JSON body and return a list of greetings. |
| `GET /health` | Return a response with no request data. |

The field tags add validation rules to the API document. The handlers enforce
those rules. specout does not parse or validate requests.

Only `Title` and `Version` are required in `Config`.

Save this as `main.go` in your Go module:

```go
package main

import (
	"encoding/json/v2"
	"errors"
	"log"
	"net/http"
	"unicode/utf8"

	"github.com/happytoolin/specout"
)

type HelloRequest struct {
	Name     string `path:"name" jsonschema:"minLength=1,maxLength=40,description=Name to greet"`
	Language string `query:"language,omitempty" jsonschema:"enum=en|es,default=en"`
}

type RepeatRequest struct {
	Name     string `json:"name" jsonschema:"minLength=1,maxLength=40"`
	Language string `json:"language,omitempty" jsonschema:"enum=en|es,default=en"`
	Times    int    `json:"times" jsonschema:"minimum=1,maximum=3"`
}

type Greeting struct {
	Message string `json:"message" jsonschema:"description=Greeting text"`
}

type GreetingList struct {
	Messages []string `json:"messages" jsonschema:"minItems=1,maxItems=3"`
}

type Problem struct {
	Error string `json:"error"`
}

type Health struct {
	Status string `json:"status" jsonschema:"enum=ok"`
}

func main() {
	doc := specout.New(specout.Config{
		Title:       "Hello API",
		Version:     "1.0.0",
		Description: "A simple greeting service.",
		Contact: &specout.Contact{
			Name:  "API team",
			Email: "api@example.com",
		},
		Servers: []specout.Server{
			{URL: "http://localhost:8080", Description: "Local server"},
		},
		Tags: []specout.Tag{
			{Name: "greetings", Description: "Greeting operations"},
			{Name: "health", Description: "Service health"},
		},
		ClosedSchemas: true,
	})
	mux := http.NewServeMux()
	routes := specout.Std(doc, mux)

	routes.Get("/hello/{name}", specout.Handler[HelloRequest, Greeting]{
		HandlerFunc: hello,
		Summary:     "Say hello",
		OperationID: "sayHello",
		Tags:        []string{"greetings"},
		Responses: []specout.Response{
			{Status: http.StatusBadRequest, Type: Problem{}},
		},
	})
	routes.Post("/greetings", specout.Handler[RepeatRequest, GreetingList]{
		HandlerFunc: repeatGreeting,
		Summary:     "Repeat a greeting",
		OperationID: "repeatGreeting",
		Tags:        []string{"greetings"},
		Responses: []specout.Response{
			{Status: http.StatusBadRequest, Type: Problem{}},
		},
	})
	routes.Get("/health", specout.Get[Health]{
		HandlerFunc: health,
		Summary:     "Check service health",
		OperationID: "getHealth",
		Tags:        []string{"health"},
	})
	mux.Handle("GET /openapi.json", doc)

	log.Fatal(http.ListenAndServe("localhost:8080", mux))
}

func hello(w http.ResponseWriter, r *http.Request) {
	input := HelloRequest{Name: r.PathValue("name"), Language: "en"}
	if values, ok := r.URL.Query()["language"]; ok {
		input.Language = values[0]
	}
	message, err := greetingMessage(input.Name, input.Language)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Problem{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, Greeting{Message: message})
}

func repeatGreeting(w http.ResponseWriter, r *http.Request) {
	input := RepeatRequest{Language: "en"}
	if err := json.UnmarshalRead(r.Body, &input, json.RejectUnknownMembers(true)); err != nil {
		writeJSON(w, http.StatusBadRequest, Problem{Error: "Invalid JSON body"})
		return
	}
	if input.Times < 1 || input.Times > 3 {
		writeJSON(w, http.StatusBadRequest, Problem{Error: "Times must be between 1 and 3"})
		return
	}
	message, err := greetingMessage(input.Name, input.Language)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Problem{Error: err.Error()})
		return
	}
	messages := make([]string, input.Times)
	for i := range input.Times {
		messages[i] = message
	}
	writeJSON(w, http.StatusOK, GreetingList{Messages: messages})
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: "ok"})
}

func greetingMessage(name, language string) (string, error) {
	if !utf8.ValidString(name) {
		return "", errors.New("Name must use valid UTF-8")
	}
	if length := utf8.RuneCountInString(name); length < 1 || length > 40 {
		return "", errors.New("Name must have 1 to 40 characters")
	}
	switch language {
	case "en":
		return "Hello, " + name, nil
	case "es":
		return "Hola, " + name, nil
	default:
		return "", errors.New("Language must be en or es")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.MarshalWrite(w, value); err != nil {
		log.Print(err)
	}
}
```

Run `go run .`.

Request a greeting in Spanish:

```sh
curl 'http://localhost:8080/hello/Ada?language=es'
```

```json
{"message":"Hola, Ada"}
```

Send a JSON body to repeat a greeting:

```sh
curl http://localhost:8080/greetings \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada","times":2}'
```

```json
{"messages":["Hello, Ada","Hello, Ada"]}
```

Send an invalid repeat count:

```sh
curl -i http://localhost:8080/greetings \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada","times":0}'
```

The handler returns `400 Bad Request` with this JSON body:

```json
{"error":"Times must be between 1 and 3"}
```

Open [localhost:8080/health](http://localhost:8080/health) to check service health.
Open [localhost:8080/openapi.json](http://localhost:8080/openapi.json) to get the API document.

### What the example shows

| Setting | Effect |
|---|---|
| `path` and `query` | Describe where request parameters come from. |
| `json` | Set the JSON field name. `omitempty` makes a field optional in the schema. |
| `minLength` and `maxLength` | Describe string length limits. |
| `minimum` and `maximum` | Describe number limits. |
| `enum` | List the allowed values. |
| `default` | Document a default value. The handler must apply it. |
| `minItems` and `maxItems` | Describe array length limits. |
| `Responses` | Add a typed `400` error response beside the inferred `200` response. |
| `specout.Get[Health]` | Describe a route with no request data and a JSON response. |

`ClosedSchemas` marks object schemas as closed. The JSON handler uses
`json.RejectUnknownMembers(true)` to reject extra fields. It also rejects
invalid JSON and checks each input value before it writes a response.

The `greetingMessage` and `writeJSON` functions are local helpers. They are part
of the example, not the specout library.

`Config.Tags` describes each group of routes. `Handler.Tags` puts a route in a
group. `OperationID` sets a stable name for client code.

`Servers` sets the base URLs in the API document. It does not set the address of
the HTTP server or change route paths.

### More configuration options

Add these fields to `specout.Config` when your API needs them:

| Field | Purpose |
|---|---|
| `TermsOfService` | Add a link to the terms of service. |
| `License` | Add the API license name and URL. |
| `ExternalDocs` | Add a link to an API guide. |
| `Auth` | Describe authentication. Your middleware must check credentials. |
| `ErrorType` and `DefaultErrors` | Describe a shared error body and its status codes. Your handlers write the error responses. |
| `ClosedSchemas` | Declare that object schemas do not allow unknown fields. Your code must enforce this rule. |

See [configuration and authentication](skills/specout/references/configuration.md)
for code examples.

## Use your router

Use `specout.Std` with `http.ServeMux`. Use `specout.Chi` with chi. Use
`specout.Gorilla` with gorilla/mux.

With chi, call `Adopt` on the outermost router after you register the routes.
This resolves paths from route groups and mounted routers.

With other routers, register the routes separately. Use `specout.Document` to
record their API contracts.

### Future router support

We are interested in adding adapters for Echo, Fiber, and other routers.
Community demand will guide this work.

Request router support in the [issue tracker](https://github.com/happytoolin/specout/issues).

## Serve or export the document

The output uses OpenAPI 3.1 and JSON Schema 2020-12. The JSON output has a stable
order.

Register all routes before you call `ServeHTTP` or `WriteJSON`. After a successful
document build, you cannot add routes.

## Check the API docs

With chi and gorilla/mux, tests can find routes that have no documentation.

`http.ServeMux` cannot list its routes. Use `specout.Std` to register each route
that needs documentation.

The optional `recorder` package can compare response statuses from HTTP tests
with the declared statuses. It does not check response bodies.

## More information

- [Runnable examples](examples/README.md).
- [Sample API document](examples/onboarding/openapi.json).
- [Request types and responses](skills/specout/references/types-and-responses.md).
- [Router setup and documentation checks](skills/specout/references/routers-and-verification.md).
- [Configuration and authentication](skills/specout/references/configuration.md).

Feedback and contributions are welcome. More router adapters are also welcome.

## Development

Read the [code guide](CONTRIBUTING.md) for build steps and the file layout.

```sh
just check  # All CI and release checks.
just lint   # Go lint rules and formatting.
just demo   # Run the onboarding example.
```

`just check` requires Go, Python 3, and Node.js with npm. It checks builds,
formatting, and lint rules. It runs tests, race checks, and vulnerability scans.
It validates API documents. It generates and compiles client types. It also
checks module files and GitHub Actions workflows.

Coding agents can use the optional [specout skill](skills/specout/SKILL.md).

Install it with this command:

```sh
npx skills add happytoolin/specout --skill specout
```

## License

[Apache 2.0](LICENSE).
