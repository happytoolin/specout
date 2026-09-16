# specout

**OpenAPI 3.1 from plain Go handlers. No comments to keep in sync. No runtime to adopt. Specs your tests can prove honest.**

Point it at the router you already have. Tag the types you already have. It emits the spec; your tests verify it.

```go
d := specout.New(specout.Config{
	Title: "Onboarding API",
	Version: "1.0.0",
})

func HandleList(deps Deps) specout.Handler[ListRequest, Page] {
	return specout.Handler[ListRequest, Page]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) { /* raw std */ },
		Summary:     "List onboarding records",
	}
}

d.Get(r, "/onboarding", HandleList(deps))
r.Mount("/openapi.json", d)
```

## Why

Specs rot because they live apart from the code. specout closes the gap from the Go side:

- **No drift.** The spec builds from the same mux that serves traffic. An undocumented route fails a test, not a review.
- **No lock-in.** Handlers stay plain `http.HandlerFunc`. No framework, no request lifecycle to adopt — or later escape.
- **Doc, nothing else.** It reads types, tags, and route patterns. It never decodes, validates, or writes a response.
- **Provable.** A test-time recorder fails CI when a handler emits a status the spec does not declare, or vice versa.
- **Byte-deterministic.** Same binary, same routes, same bytes. Diffs are reviewable; the spec commits like code.

OpenAPI 3.1 (JSON Schema 2020-12), Go 1.27+, two dependencies: chi and the reflection layer.

## Install

Requires Go 1.27 or newer:

```sh
go get github.com/happytoolin/specout@latest
```

## The idea in one endpoint

Metadata rides the generic return type. Reflection sees it at registration; the handler itself stays untouched std:

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
			api.JSON(w, 200, onb)               // your helper, not ours
		},
		Summary: "Create or replace a record",
		Responses: []specout.Response{{Status: 422, Type: api.ValidationError{}}},
	}
}
```

Everything the spec needs is visible in the source you already write:

| Spec feature | How you declare it |
|---|---|
| path params | route pattern `{id}` |
| query/header/cookie params | `query:` / `header:` / `cookie:` tags on `Req` |
| required vs optional | presence of `omitempty` |
| nullable | Go pointer (`type: [T, "null"]`) |
| enums, bounds, patterns, formats | `jsonschema:` tag |
| oneOf unions with discriminator | `d.Register[T]("name")` + `oneof_type` tag |
| readOnly / writeOnly | `jsonschema:` tag |
| auth schemes | `Config.Auth` (Bearer, API key header/cookie); `Public: true` opts out |
| operationId | derived from method+path, or set `OperationID` |

Two types with the same name panic at build time, with the fix in the message:

```
panic: specout: duplicate component name Widget (pa.Widget vs pb.Widget), call SchemaName to disambiguate
```

## Responses beyond 200

```go
Responses: []specout.Response{
	{Status: 200, ContentType: "application/pdf"}, // binary download
	{Status: 201, Headers: []specout.Header{{Name: "Location"}}},
	{Status: 409, Type: SyncConflict{}},       // per-route error shape
	{Status: 401, Omit: true},                 // drop one default error code
}
```

## Verify it in CI

Two tests keep the doc honest — every route documented, every declared status actually produced:

```go
func TestEveryRouteIsDocumented(t *testing.T) { specout.RequireDocumented(t, r) }

func TestSpecMatchesReality(t *testing.T) {
	d, r := router.New()
	rec := recorder.New(r)
	// ... hit every route through rec
	recorder.Verify(t, d, rec)
}
```

The golden export is committed — same binary, byte-identical output, checked in CI along with validation against the official OpenAPI 3.1 schema:

```sh
GO_SPEC_ONLY=1 go run ./cmd/demo > openapi.json
git diff --exit-code openapi.json
```

## Demo

```sh
git clone https://github.com/happytoolin/specout && cd specout
just demo   # or: go run ./cmd/demo
```

Swagger UI at http://localhost:8080/, Scalar at /scalar, Redoc at /redoc, the spec at /openapi.json.

| Route | What it shows |
|---|---|
| `GET /onboarding` | cookie + header + query params on one request type |
| `PUT /onboarding/{id}` | multi-status upsert (200/201), Location header, 422 override |
| `POST /onboarding/{id}/sync` | optimistic concurrency, rich 409 body |
| `POST /files/import` | file upload → multipart/form-data |
| `GET /files/report` | binary response (PDF) |
| `POST /webhooks`, `POST /channels` | oneOf unions with discriminators |
| `POST /things` | every constraint keyword in one body |
| `GET /legacy` | `Deprecated: true` |

## Design rules

- **Lazy build, freeze on first serve.** Registering after the spec is served panics.
- **No package globals.** One generator per service; deterministic output by construction.
- **chi and std mux both work.** `d.Get(r, ...)` / `d.Handle(mux, "GET /path", ...)`.
- **No comment parsing, ever.** Types are the single source of truth.

## Docs

- [docs/api-reference.html](docs/api-reference.html) — the full public surface with generated-output examples
- [docs/design-discussion.md](docs/design-discussion.md) — rationale and trade-offs
- [PLAN.md](PLAN.md) — implementation log and decision ledger

## License

MIT
