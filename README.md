# specout

**OpenAPI 3.1 from plain Go handlers. No comments to keep in sync. No runtime to adopt. Specs your tests can prove honest.**

Point it at the router you already have. Tag the types you already have. It emits the spec; your tests verify it.

```go
d := specout.New(specout.Config{
	Title: "Onboarding API",
	Version: "1.0.0",
})
```

```go
func HandleList(deps Deps) specout.Handler[ListRequest, Page] {
	return specout.Handler[ListRequest, Page]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) { /* raw std */ },
		Summary:     "List onboarding records",
	}
}

rc := specout.Chi(d, r)
rc.Get("/onboarding", HandleList(deps))
if err := rc.Adopt(); err != nil { panic(err) }
r.Mount("/openapi.json", d)
```

## Why

Specs rot because they live apart from the code. specout closes the gap from the Go side:

- **Route checks.** The chi and gorilla adapters scan the live router for undocumented routes. Std mux and other routers use explicit declarations.
- **No lock-in.** Handlers stay plain `http.HandlerFunc`. No framework, no request lifecycle to adopt — or later escape.
- **Doc, nothing else.** It reads types, tags, and route patterns. It never decodes, validates, or writes a response.
- **Provable.** A test-time recorder fails CI when a handler emits a status the spec does not declare, or vice versa.
- **Byte-deterministic.** Same binary, same routes, same bytes. Diffs are reviewable; the spec commits like code.

OpenAPI 3.1 (JSON Schema 2020-12), Go 1.27+. Three runtime dependencies: chi,
gorilla/mux and the reflection layer. testify is a test-only dependency.

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

The same metadata has a fluent spelling — chains copy, so factories can build up a base and callers extend it:

```go
func HandleUpsert(d Deps) specout.Handler[UpsertRequest, Onboarding] {
	return specout.Handler[UpsertRequest, Onboarding]{HandlerFunc: upsert(d)}.
		WithSummary("Create or replace a record").
		WithTags("onboarding").
		WithResponse(specout.Response{Status: 409, Type: SyncConflict{}})
}
```

Both spellings are the same type: literal fields where metadata is static, the chain for composing or extending shared bases. Every link returns a copy.

## Shape aliases

The empty sides of `Handler` need no spelling out. Two aliases cover them; both are full type aliases, so every binder and `With*` method accepts them unchanged:

```go
specout.Get[Onboarding]{HandlerFunc: fetch, Summary: "Fetch one record"}      // = Handler[struct{}, Onboarding]
specout.Delete{HandlerFunc: remove, Summary: "Delete an onboarding record"}  // = Handler[struct{}, NoContent]
```

Everything the spec needs is visible in the source you already write:

| Spec feature | How you declare it |
|---|---|
| path params | route pattern `{id}` |
| query/header/cookie params | `query:` / `header:` / `cookie:` tags on `Req` |
| required vs optional | presence of `omitempty` or `omitzero` |
| nullable | Go pointer (type union for scalars; `anyOf` for references) |
| enums, bounds, patterns, formats | `jsonschema:` tag |
| oneOf unions with discriminator | `d.Register[T]("name")` + `oneof_type` tag |
| readOnly / writeOnly | `jsonschema:` tag |
| auth schemes | `Config.Auth` (Bearer, API key, `OAuth2`, openIdConnect); `Public: true` opts out |
| operationId | derived from method+path, or set `OperationID` |
| status ranges | `{Key: "4XX"}` — one entry for a whole range; a concrete `Status` beside it is the code tests must produce |
| several content types | `RequestContentTypes` on the handler, `ContentTypes` on a response |
| parameter serialization | `jsonschema:"style=deepObject,explode=true"` |
| deprecated property | `jsonschema:"deprecated"` |
| `x-` extensions | `jsonschema_extras` on a field, `Raw` on a handler |
| `examples` (plural) | repeat `example=` in the tag |
| contact, license, terms | `Config.Contact` / `License` / `TermsOfService` |
| externalDocs | `ExternalDocs` on `Config`, a handler, or a tag |

Two types with the same name panic at build time, with the fix in the message:
the check covers nested types too, not only request and response bodies.

```
panic: specout: duplicate component name Widget (pa.Widget vs pb.Widget), call SchemaName to disambiguate
```

## Responses beyond 200

```go
Responses: []specout.Response{
	{Status: 200, ContentType: "application/pdf"}, // binary download
	{Status: 200, ContentTypes: []string{"application/json", "application/xml"}},
	{Status: 201, Headers: []specout.Header{{Name: "Location"}}},
	{Status: 409, Type: SyncConflict{}},       // per-route error shape
	{Status: 401, Omit: true},                 // drop one default error code
	{Key: "4XX", Type: Problem{}},              // one entry for a whole range
}
```

## Verify it in CI

Two tests keep the doc honest — every route documented, every declared status actually produced:

```go
func TestEveryRouteIsDocumented(t *testing.T) { specout.Chi(d, r).RequireDocumented(t) }

func TestSpecMatchesReality(t *testing.T) {
	d, r := router.New()
	// skip routes that are not API operations, e.g. the spec endpoint
	rec := recorder.New(r, specout.Skip("/openapi.json"))
	// ... hit every route through rec
	recorder.Verify(t, d, rec)
}
```

The golden export is committed. CI checks its bytes, runs race tests, and
validates generated test documents and examples against OpenAPI 3.1:

```sh
just golden
git diff --exit-code examples/onboarding/openapi.json
```

Run `just validate` to validate generated documents against OpenAPI 3.1 and
JSON Schema 2020-12. Run `just client-types` to generate and compile TypeScript
types from all served examples.

## Demo

```sh
git clone https://github.com/happytoolin/specout && cd specout
just demo   # or: go run ./examples/onboarding
```

Swagger UI is at http://localhost:8080/. The spec is at `/openapi.json`.

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
- **One generator per service.** Route and schema registrations stay on that generator.
- **chi, gorilla and std mux all work.** `specout.Chi(d, r).Get(...)` / `specout.Gorilla(d, gm).Get(...)` / `specout.Std(d, mux).Handle("GET /path", ...)`. Other routers record through `specout.Document`.
- **No comment parsing, ever.** Types are the single source of truth.

## How the pieces connect

1. `Handler[Req, Res]` carries the handler and its document metadata.
2. `Chi`, `Gorilla`, `Std`, or `Document` records one internal route entry.
3. The first spec read resolves live router paths and reflects request and
   response schemas.
4. The generator freezes and serves deterministic OpenAPI JSON.
5. `recorder.Verify` compares declared status codes with codes observed in
   tests.

The core files follow that flow: `routes.go` records routes, `build.go`
assembles the document, `schema.go` and `fixups.go` own schema reflection, and
`recorder/` owns runtime verification. Router-specific code stays in the three
`register_*.go` files.

## More examples

See [examples/README.md](examples/README.md) for the onboarding, Petstore, and
Microsoft Graph examples. Public types and methods have Go documentation in
the source.

## License

MIT
