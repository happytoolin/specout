# Reddit announcement

Attach [`specout-v0.0.1-og.png`](specout-v0.0.1-og.png) to the post.

## Title

specout v0.0.1 — OpenAPI 3.1 from plain Go HTTP handlers

## Body

I released the first public version of specout, a Go library that generates deterministic OpenAPI 3.1 documents from normal HTTP handlers.

I built it because I wanted API documentation without comment annotations, generated handlers, or a second routing system. The application still owns request decoding, validation, and responses.

The current release supports `net/http`, `chi/v5`, and `gorilla/mux`. Request and response schemas come from Go types and field tags. The test helpers can compare documented routes with the live router and declared response statuses with statuses observed during tests.

```bash
go get github.com/happytoolin/specout@v0.0.1
```

Repository: https://github.com/happytoolin/specout

This is still pre-1.0. I am especially interested in feedback about the registration API, schema edge cases, HTTP behavior that is not covered, and routers that need adapters. Contributions are welcome.

If you use a different router, which one should specout support next?
