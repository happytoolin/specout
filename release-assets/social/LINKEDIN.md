# specout LinkedIn post

## Post

I wanted OpenAPI docs for our Go APIs without changing how we write HTTP handlers.

So I built specout.

It generates OpenAPI from Go request and response types. You keep your handlers, router, and middleware. Your code still controls request parsing, validation, headers, and responses.

The library supports net/http, chi, and gorilla/mux. You describe the contract with types and field tags, then attach that metadata to your handlers.

It also provides checks for a common problem: API docs that no longer match the service. With chi and gorilla/mux, tests can find undocumented routes. An optional recorder compares declared response statuses with the statuses returned during HTTP tests.

The project is open source. I would like feedback from Go developers who want to add API docs to existing services.

Which router should I add next?

https://github.com/happytoolin/specout

#Golang #OpenAPI #OpenSource

## Short alternative

Introducing specout: OpenAPI from Go types, with your usual HTTP handlers.

It supports net/http, chi, and gorilla/mux. Your application still controls request parsing, validation, and responses. Test helpers can check route documentation and observed response statuses.

I would like feedback on the API, generated schemas, and router support.

https://github.com/happytoolin/specout

#Golang #OpenAPI #OpenSource

## Image

Use [specout-square.png](specout-square.png) for the main post. [specout-portrait.png](specout-portrait.png) provides a taller alternative with the same message.

**Alt text:** specout. OpenAPI from Go types. Keep your HTTP handlers. A black document symbol and a turquoise square illustrate code producing API documentation. The background is off-white.
