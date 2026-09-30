# specout Reddit post

## Title

I built specout to generate OpenAPI from Go types and keep plain HTTP handlers

## Post

I built specout because I wanted OpenAPI docs without changing how I write Go HTTP handlers.

It uses request and response types, plus field tags, to describe the API contract. Your handler still receives `http.ResponseWriter` and `*http.Request`. Your code handles request parsing, validation, headers, and responses.

The library supports:

- `net/http`, chi, and gorilla/mux.
- Request and response schemas from Go types.
- Checks for undocumented routes with chi and gorilla/mux.
- An optional test recorder that compares declared response statuses with the statuses your handlers return during tests.

There are no comment annotations or generated handlers to maintain. You attach the request and response types to a handler through a small metadata wrapper.

The metadata is explicit, so you still need to keep it correct. Tags describe the contract; your application must enforce it. The response recorder checks status codes, not response bodies.

Repository: https://github.com/happytoolin/specout

I would like feedback on the registration API, generated schemas, and router support. Which router do you use, and would this approach fit your service?

## Short post for a project thread

I built **specout**, a Go library that generates OpenAPI from request and response types. You keep your plain HTTP handlers and control request parsing, validation, and responses.

It supports `net/http`, chi, and gorilla/mux. Test helpers can check for undocumented routes with chi and gorilla/mux. An optional recorder checks declared response statuses against the statuses observed during HTTP tests.

Repository: https://github.com/happytoolin/specout

I would like feedback on the API and generated schemas. Which router should I add next?

## Image

Use [specout-wide.png](specout-wide.png) if the selected post format supports images. Use the text alone for a project thread comment.

**Alt text:** specout. OpenAPI from Go types. Keep your HTTP handlers. A flat document symbol sits beside a turquoise square and an arrow. The background is off-white.
