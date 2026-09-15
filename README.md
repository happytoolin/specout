# specout

**OpenAPI 3.1 from plain Go handlers. No comments. Doc-only. Verified by your own tests.**

```go
d := specout.New(specout.Config{
    Title:         "Onboarding API",
    Version:       "1.0.0",
    Auth:          specout.Bearer,
    ErrorType:     api.Problem{},
    DefaultErrors: []int{400, 401, 403, 404, 409, 500},
})

d.Get(r, "/onboarding", handlers.HandleList(deps))
r.Mount("/openapi.json", d)
```

Handlers stay raw `http.HandlerFunc` factories. The generic return type carries the
schemas. Registration is compile-checked (Go 1.27+). The spec builds from the live
router, so it cannot drift. A recorder in your test suite fails CI when reality and
declaration disagree.

## The contract

specout is **doc, nothing else**. It reads what reflection can see — types, tags,
route patterns — and emits the spec. It never touches a request or a response:

- no decoding, no responding, no error mapping, no middleware
- runtime helpers are your code (`internal/demo/api` is ~40 lines; copy it)
- everything specout ships is a declaration: types, tags, config

## Quick start

```sh
go get github.com/happytoolin/specout@latest
```

A handler factory:

```go
type UpsertRequest struct {
    Owner string `json:"owner" jsonschema:"format=email"`
    Stage string `json:"stage" jsonschema:"enum=draft|active|archived,default=draft"`
}

func HandleUpsert(d Deps) specout.Handler[UpsertRequest, Onboarding] {
    return specout.Handler[UpsertRequest, Onboarding]{
        HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
            var req UpsertRequest
            json.NewDecoder(r.Body).Decode(&req) // your code, your way
            // ... store, validate, write — all yours
            api.JSON(w, 200, onb) // your helper
        },
        Summary: "Create or replace a record",
        Tags:    []string{"onboarding"},
        Responses: []specout.Response{
            {Status: 201, Headers: []specout.Header{{Name: "Location"}}},
            {Status: 422, Type: api.ValidationError{}},
        },
    }
}
```

Register it — the verb call is the only custom bit:

```go
r.Route("/onboarding/{id}", func(r chi.Router) {
    d.Put(r, "/", HandleUpsert(deps))
})
r.Mount("/openapi.json", d)
```

Std mux works the same: `d.Handle(mux, "PUT /onboarding/{id}", HandleUpsert(deps))`.

## What it documents

**Parameters** — from tags on the request type:

```go
type ListRequest struct {
    Session string `cookie:"session"    jsonschema:"description=Session cookie"`
    Trace   string `header:"X-Trace-Id" jsonschema:"description=Trace id"`
    Limit   int    `query:"limit"      jsonschema:"default=20,minimum=1,maximum=100"`
    Sort    string `query:"sort"       jsonschema:"enum=created|updated,default=created"`
}
```

Path params (`{id}`) come from the route pattern automatically.

**Schemas** — from Go types via reflection, deduped into components:

- enums, formats, bounds, patterns, examples — all through the `jsonschema` tag
- required = no `omitempty`; nullable = pointers
- `readOnly`/`writeOnly` for shared request/response types
- oneOf unions with discriminators (`d.Register[EmailConfig]("email")` + `oneof_type` tag)
- `JSONSchema()` method on a type for full control
- `ClosedSchemas: true` for `additionalProperties: false`

**Responses** — `Res` defaults to 200 + schema; `specout.NoContent` (or `struct{}`) means 204;
explicit entries add codes, change shapes, declare headers or binary bodies:

```go
Responses: []specout.Response{
    {Status: 200, ContentType: "application/pdf"},        // binary download
    {Status: 409, Type: SyncConflict{}},                  // per-route error shape
    {Status: 401, Omit: true},                            // this route is public
}
```

**Auth** — declare once, every operation inherits it:

```go
Auth: specout.Bearer                              // http bearer
Auth: specout.APIKey("session", specout.InCookie) // api key: header/query/cookie
```

Public routes opt out with `Public: true` on the Handler.

**Operations** — `Summary`, `Description`, `Tags`, `Deprecated` on each Handler;
`operationId` derives deterministically from method+path (`getOnboardingId`).

## Demo

```sh
go run ./cmd/demo
# swagger ui: http://localhost:8080/
# scalar:      http://localhost:8080/scalar
# spec:        http://localhost:8080/openapi.json
```

A realistic multi-package service — every route shape in one app:

| Route | What it shows |
|---|---|
| `GET /onboarding` | cookie + header + query params on one request type |
| `GET /onboarding/{id}` | path params, `Public: true` (no auth needed) |
| `PUT /onboarding/{id}` | multi-status upsert (200/201), Location header, 422 override |
| `POST /onboarding/{id}/sync` | optimistic concurrency, rich 409 body, 401 omitted, Description |
| `POST /files/import` | `specout.File` field → multipart/form-data |
| `GET /files/report` | binary response (`ContentType: "application/pdf"`) |
| `POST /webhooks` | oneOf union with discriminator, enum-tagged kind |
| `GET /legacy` | `Deprecated: true` |

Layout — app code, no library involvement beyond declarations:

```
internal/demo/
├── api/          # the app's own helpers: JSON, Error, Problem, mapper
├── onboarding/   # domain types + store (typed ConflictError)
├── webhooks/     # union variants
├── handlers/     # raw http.HandlerFunc factories per resource
└── router/       # wiring: config, groups, Adopt, spec mount
```

## Verify the spec in CI

Two tests keep the doc honest:

```go
// every route is documented
func TestEveryRouteIsDocumented(t *testing.T) {
    specout.RequireDocumented(t, r)
}

// declared statuses == observed statuses
func TestSpecMatchesReality(t *testing.T) {
    d, r := router.New()
    rec := recorder.New(r)
    // ... hit every route through rec
    recorder.Verify(t, d, rec)
}
```

And the golden export — same binary, byte-identical output:

```sh
GO_SPEC_ONLY=1 go run ./cmd/demo > openapi.json
git diff --exit-code openapi.json
```

## Design

- **Lazy build, freeze on first serve.** Registering after the spec is served panics.
- **Byte-deterministic.** Same binary + same routes → identical bytes. Key order is
  insertion order, never map order.
- **Two routers.** chi (walk-based path resolution; `d.Adopt(r)` for existing routers)
  and std `http.ServeMux` (`d.Handle` with method+wildcard patterns).
- **Go 1.27+ only.** Generic verb methods make an undocumented route a compile error.

## Status

- [x] Phase 0–6 complete; see [PLAN.md](PLAN.md) for the decision ledger
- [ ] v0 tag after real-project feedback

Docs: [api-reference.html](docs/api-reference.html) (full public surface) ·
[design-discussion.md](docs/design-discussion.md) (rationale) ·
[PLAN.md](PLAN.md) (implementation log)
