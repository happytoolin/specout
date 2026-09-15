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

- [PLAN.md](PLAN.md) — implementation plan, decision ledger, acceptance criteria
- [docs/api-reference.html](docs/api-reference.html) — full public API reference
- [docs/design-discussion.md](docs/design-discussion.md) — design conversation & rationale

Routers: std `http.ServeMux` + chi. Status: design complete, implementation starting.
