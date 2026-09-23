# specout

![specout — OpenAPI 3.1 from plain Go handlers](release-assets/v0.0.1/specout-v0.0.1-og.png)

Generate OpenAPI 3.1 from Go types. Keep your usual HTTP handlers.

Your code reads the request, validates input, and writes the response. specout
uses the types and tags you provide to document that contract.

## Why we built it

For our day-to-day APIs, we wanted useful docs without changing how we handle
HTTP. We wanted to keep our routers, middleware, validation, and error responses.

Tools such as Huma and Fuego connect API documentation with request handling:

- [Huma](https://huma.rocks/features/operations/) uses handlers shaped like
  `func(context.Context, *Input) (*Output, error)`. Its input and output models
  describe parameters, headers, and bodies. The framework handles parsing,
  validation, and response encoding.
- [Fuego](https://github.com/go-fuego/fuego/blob/main/documentation/docs/guides/controllers.md)
  normally uses its own context types, such as `fuego.ContextWithBody[T]`.
  Handlers return a response and an error. The framework provides body decoding
  and [response encoding](https://github.com/go-fuego/fuego/blob/main/documentation/docs/guides/serialization.md).

Those conventions can save work. Both also allow lower-level HTTP access through
[Huma's adapters](https://huma.rocks/features/middleware/#unwrapping) and
[Fuego's standard handlers](https://github.com/go-fuego/fuego/blob/main/documentation/docs/guides/routing.md).
The part we did not want was adopting another handler model just to get docs.

So we kept the part we needed: describe the request and response with Go types,
attach them to an existing `http.HandlerFunc`, and generate the specification.
The handler still gets `http.ResponseWriter` and `*http.Request` directly.
It decides how to read the body, check input, set headers, stream data, and return
errors. Existing middleware and HTTP tests still work.

specout adds a metadata wrapper, `Handler[Request, Response]`. It does not decode
or validate requests for you. Tags describe the contract; your code must enforce
it. There are no comment annotations or generated handlers to maintain.

## Install

Requires **Go 1.27 or later**. The API is pre-1.0.

```sh
go get github.com/happytoolin/specout@v0.0.1
```

## A small example

Save this as `main.go` in your Go module:

```go
package main

import (
	"encoding/json/v2"
	"log"
	"net/http"
	"time"

	"github.com/happytoolin/specout"
)

type HelloRequest struct {
	Name string `path:"name"`
}

type Greeting struct {
	Message string `json:"message"`
}

func hello(w http.ResponseWriter, r *http.Request) {
	message := Greeting{Message: "Hello, " + r.PathValue("name")}
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, message); err != nil {
		log.Print(err)
	}
}

func main() {
	doc := specout.New(specout.Config{Title: "Hello API", Version: "1.0.0"})
	mux := http.NewServeMux()
	specout.Std(doc, mux).Get("/hello/{name}", specout.Handler[HelloRequest, Greeting]{
		HandlerFunc: hello,
		Summary:     "Say hello",
	})
	mux.Handle("/openapi.json", doc)

	server := &http.Server{
		Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
```

Run `go run .`. Visit `/hello/Ada` for the response and `/openapi.json` for the
specification. `HelloRequest` documents the path parameter. `Greeting` documents
the JSON response. The `hello` function does the actual HTTP work.

## Use it in your service

Adapters support `net/http`, `chi/v5`, and `gorilla/mux`. With chi, call `Adopt`
on the outermost router after registration to resolve grouped and mounted paths.
Other routers can use `specout.Document` to record routes registered separately.

The output is deterministic OpenAPI 3.1 with JSON Schema 2020-12. Register all
routes before the first call to `ServeHTTP` or `WriteJSON`; that call freezes
the document.

Tests can check for undocumented routes with chi and gorilla/mux. The optional
`recorder` package compares declared response statuses with those observed in
HTTP tests. It does not validate response bodies. `http.ServeMux` cannot list its
routes, so register documented endpoints through `specout.Std`.

- [Runnable examples](examples/README.md) and a [sample specification](examples/onboarding/openapi.json).
- [Request types, tags, and responses](skills/specout/references/types-and-responses.md).
- [Router setup and contract tests](skills/specout/references/routers-and-verification.md).
- [Configuration and authentication metadata](skills/specout/references/configuration.md).

More router adapters and feedback from real services are welcome.

## Development

```sh
just check  # All CI and release checks.
just lint   # Go lint rules and formatting.
just demo   # Run the onboarding example.
```

`just check` needs Go, Python 3, and Node.js/npm. It runs builds, linting, tests,
race checks, OpenAPI validation, client type compilation, and vulnerability scans.
It also checks module files and GitHub Actions workflows.

For coding agents, an optional [specout skill](skills/specout/SKILL.md) is available:

```sh
npx skills add happytoolin/specout --skill specout
```

## License

[Apache 2.0](LICENSE).
