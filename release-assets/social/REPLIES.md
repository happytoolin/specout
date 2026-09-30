# specout launch replies

These replies cover likely questions after the posts. Use the reply that matches the question.

## How is this different from an API framework

I wanted to keep normal Go handlers and add documentation around them. specout records request and response metadata. Your application still parses requests, validates input, and writes responses. It fits that choice of design.

## Does it validate requests

No. Types and tags describe the API contract. Your handler must enforce it. specout does not parse or validate requests for you.

## How does it check that the docs match the service

With chi and gorilla/mux, route checks can find endpoints that are not documented. The optional recorder compares declared response statuses with statuses observed during HTTP tests. It does not validate response bodies or prove that the entire contract is correct.

## Can I use another router

The built-in adapters support net/http, chi, and gorilla/mux. Other routers can register their routes separately and use specout.Document to record their contracts. I would like to know which router you need.

## Does it require comments or generated handlers

No. Request and response types carry the schema information. Field tags describe parameters and bodies. You attach the types to your existing handler through specout.Handler.

## Where can I see a working example

The repository has runnable examples for a small API, a pet store, and a Microsoft Graph subset. Each example serves a generated specification and Swagger UI.

https://github.com/happytoolin/specout/tree/main/examples

## Is it free to use

Yes. specout is open source under the Apache License. The license is in the repository.

https://github.com/happytoolin/specout/blob/main/LICENSE
