# specout

**OpenAPI 3.1 from plain Go handlers. No comments. Verified by your own tests.**

```go
d := specout.New(specout.Config{
    Title:         "Onboarding API",
    Version:       "1.0.0",
    ErrorType:     api.Problem{},
    DefaultErrors: []int{400, 401, 403, 404, 409, 500},
})

d.Get(r, "/onboarding", handlers.HandleListOnboarding(deps))
r.Mount("/openapi.json", d)
```

Handlers stay raw `http.HandlerFunc` factories; the generic return type carries the schemas;
registration is compile-checked (Go 1.27+); the spec builds from the live router so it can't
drift; a recorder in your test suite fails CI when reality and declaration disagree.

## Demo

```sh
go run ./cmd/demo
# swagger ui: http://localhost:8080/
# spec:        http://localhost:8080/openapi.json
```

The demo mounts every route shape the library supports: CRUD with groups and
subrouters, query params, multi-status upsert, per-route error overrides and
omissions, file upload/download, unions with discriminator, readOnly/writeOnly
shared types, custom scalar schemas via JSONSchema(), and a deprecated route.

CI export from the same binary:

```sh
GO_SPEC_ONLY=1 go run ./cmd/demo > openapi.json
```

## Status

- [x] Phase 0 — scaffold, CI
- [x] Phase 1 — core loop: registration, walk stitching, deterministic serve, golden fixture
- [x] Phase 2 — schema layer: tags, dialects, nullable, ClosedSchemas, unions, JSONSchema()
- [x] Phase 3 — responses: merge rules, ContentType binary, Raw splice
- [x] Phase 4 — library ships no runtime helpers; apps write their own `api` layer
- [x] Phase 5 — verification: recorder.Verify, RequireDocumented
- [x] Phase 6 — README polish, golden-diff CI job

- [PLAN.md](PLAN.md) — implementation plan, decision ledger, acceptance criteria
- [docs/api-reference.html](docs/api-reference.html) — full public API reference
- [docs/design-discussion.md](docs/design-discussion.md) — design conversation & rationale

Routers: std `http.ServeMux` + chi.
