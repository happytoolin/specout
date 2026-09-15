# specout — implementation plan

**specout** — OpenAPI 3.1 from plain Go handlers. No comments, no drift, verified by your own tests.

- Status: design complete (see `docs/api-reference.html`), implementation not started
- Target: Go **1.27+** only (generic methods, `encoding/json/v2`, stdlib `uuid`)
- Routers: **std `http.ServeMux` + chi** (blessed pair); adapters for gin/echo/fiber are explicitly later
- Consumers: happytoolin services import this module; OSS publication is a possible later step (name is SEO-clean — nothing else owns "specout")
- Design rationale & history: `docs/design-discussion.md` · full public API: `docs/api-reference.html`

---

## 1. Non-negotiable design properties

These came out of the design conversation and must survive implementation shortcuts:

1. **Handlers stay raw std.** `specout.Handler[Req, Res]` embeds `http.HandlerFunc`; the closure sees `(w, r)` and nothing else.
2. **Reification, not parsing.** Schemas come from real types via reflection (`Types()` on the generic wrapper). No AST analysis, ever.
3. **Compile-checked discovery.** Registration verb methods are generic; passing a plain `http.HandlerFunc` fails to compile.
4. **Spec from the live router.** Full paths resolve by walking the actual chi tree at build time; std patterns are full by construction. The served spec cannot drift from the router.
5. **No package globals.** The error mapper lives on a `*specoutapi.Responder` wired through the app's deps.
6. **Structs for data, methods for behavior, generics only where the compiler must know.** Config struct, response literals, generic knobs as methods.
7. **Panic on programmer misuse** (registration after freeze), `error` only for genuinely fallible ops (build, IO).
8. **Deterministic output.** Same binary + same registrations → byte-identical `openapi.json`, so CI can golden-diff it like code.

## 2. Module layout

```
specout/
├── go.mod                        # module github.com/happytoolin/specout, go 1.27
├── specout.go                    # Generator type, New(Config), lifecycle (lazy build, freeze, panics)
├── config.go                     # Config, Server, Tag, AuthScheme, Dialect constants
├── handler.go                    # Handler[Req, Res], Documented, Types() reification
├── response.go                   # Response{Status,Type,Headers,Raw,Omit}, Header, NoContent, File, Binary, Variant, SkipRule/Skip
├── register_chi.go               # Get/Head/Post/Put/Patch/Delete/Options/Trace + Adopt (chi)
├── register_std.go               # Handle(*http.ServeMux, pattern, h)
├── routes.go                     # internal route record, handler-pointer keying, merge at build
├── build.go                      # spec assembly: tree, operation construction, default-error stamping, Omit, Raw merge
├── schema.go                     # invopop/jsonschema wiring, json/v2+v1 tag dialects, nullable pointer policy, ClosedSchemas, readOnly/writeOnly, JSONSchema() escape
├── unions.go                     # Register[T] variants, Variant discriminator, oneof_type resolution
├── params.go                     # path param extraction ({id}, {x...}), query params from Req `query:` tags
├── serve.go                      # ServeHTTP (GET only), WriteJSON, deterministic marshaling
├── verify.go                     # RequireDocumented
├── specoutapi/                   # runtime helpers (imported by apps as their `api` layer)
│   ├── responder.go              # New(Config{ErrorMapper}), OK/Status/NoContent/Err, DetailedError
│   ├── decode.go                 # Decode[Req](r): json/v2 body + query-tag fields
│   ├── problem.go                # Problem (RFC 9457)
│   └── file.go                   # SendFile
├── recorder/                     # test machinery (mirrors net/http/httptest)
│   ├── recorder.go               # New(next), pattern capture (chi RouteContext / std r.Pattern), code+body observation
│   └── verify.go                 # Verify(t, d, rec), Examples()
└── internal/testapp/             # shared sample app used by tests and golden fixtures
```

Dependency rule: **no chi imports outside `register_chi.go` / `routes.go` (walk) / `recorder`** — keeps a future `specout/chi` or adapter extraction mechanical. The `api` package name at call sites (`deps.API.OK(...)`) is the app's field name; the module ships it as `specoutapi` so apps alias or wrap as they like.

## 3. Phases

### Phase 0 — scaffold (half a day)
- [ ] `go.mod` (go 1.27), directory skeleton, `.golangci.yml` (vet, staticcheck)
- [ ] CI: build + test + `gofmt -l` check; golden-diff job placeholder
- **Done when:** empty module builds, CI green.

### Phase 1 — core loop (2–3 days)
The minimal path from factory to served spec:
- [ ] `Handler[Req, Res]` + embedded `http.HandlerFunc` + `Types()` reification
- [ ] Registration: chi verb methods (generic, type-inferred) + `Handle` for std mux; internal route records keyed by handler func pointer
- [ ] Build: lazy on first `ServeHTTP`/`WriteJSON`, then frozen; post-freeze registration panics
- [ ] Path resolution: single `chi.Walk` at build time stitches full paths to registrations; std patterns taken as-is; unresolved registrations → build error listing them
- [ ] Minimal spec assembly: paths, `Res` → 200 + `$ref`, `NoContent` → 204, components dedupe by Go type
- [ ] `ServeHTTP` (GET only) + `WriteJSON` with **byte-deterministic** output (ordered keys; see risks)
- **Tests:** unit (reification, freeze panics, walk stitching incl. `r.Route` groups and `r.Mount` subrouters), golden JSON fixture from `internal/testapp`
- **Done when:** the api-reference quick start compiles and serves a valid OpenAPI 3.1 doc.

### Phase 2 — schema layer (3–4 days)
- [ ] invopop/jsonschema reflector wiring; type-keyed component registry; `SchemaName[T]` override
- [ ] Tag dialects: json/v2 default (`(omitzero)`, case-sensitive), v1 opt-in (`Config.JSONDialect`)
- [ ] Pointer → nullable policy; `ClosedSchemas`; `readOnly`/`writeOnly`
- [ ] Field vocabulary passthrough: enum/default/format/pattern/bounds/examples
- [ ] Query params: `query:`-tagged fields in `Req` become operation parameters (not body)
- [ ] Unions: `Register[T](name)`, `Variant` discriminator, `oneof_type` tag → `oneOf` + `discriminator`
- [ ] `JSONSchema()` self-describing types; `Raw` map merge at any node
- **Tests:** golden schemas per feature; name-collision override; recursive types via `$defs`
- **Done when:** the §07/§10 tag tables and union YAML in the api-reference generate exactly.

### Phase 3 — responses & errors (2 days)
- [ ] `Response` merge rules: unmentioned error codes ← global `ErrorType`; entries ≥ 400 override per route; `Omit: true` removes a code
- [ ] Response `Headers`; `File` (multipart/binary request) and `Binary` (binary response) conventions
- [ ] `InjectExamples`
- **Tests:** upsert (4 response types) and sync (override + Omit) fixtures from api-reference §04/§05, byte-for-byte
- **Done when:** both fixtures match.

### Phase 4 — runtime helpers `specoutapi` (1–2 days)
- [ ] `Responder` with `Config{ErrorMapper}`; `OK/Status/NoContent/Err`
- [ ] `Err` flow: `DetailedError` (HTTPStatus/Payload) first, mapper second
- [ ] `Decode[Req]` (json/v2 body + query tags), `SendFile`, `Problem`
- **Tests:** mapper wiring without globals — two responders in one process; `Decode` dialect tests
- **Done when:** api-reference §06 behavior matches.

### Phase 5 — verification layer (2–3 days)
- [ ] `recorder.New(next)`: wrap, capture matched pattern (chi `RouteContext().RoutePattern()`; std `r.Pattern`) + status codes + response bodies
- [ ] `recorder.Verify(t, d, rec)`: declared ⊆ observed and observed ⊆ declared, with actionable failure messages naming route + code
- [ ] `RequireDocumented(t, r, skips...)`: chi walk; documents the std asymmetry (direct-to-mux strays invisible — convention + review)
- **Tests:** inject drift both directions, assert failures fire
- **Done when:** recorder catches a deliberately lying handler in CI.

### Phase 6 — CI export + polish (1 day)
- [ ] `GO_SPEC_ONLY=1` convention documented + `WriteJSON(os.Stdout)`; golden-file diff in CI (spec changes must be reviewed like code)
- [ ] README with quick start; badges; link the api-reference
- **Done when:** CI fails on an uncommitted spec change.

### Later (explicitly out of v0)
- gin/echo adapters (~150 lines each: Handler wrapper, verbs, `Routes()` enumeration, `:id`→`{id}`) — on demand
- fiber v2/v3 — only with eyes open (fasthttp means parallel decode/respond/recorder)
- Swagger UI serving, OpenAPI 3.0 emission, `DeriveTags` if the heuristic earns it

## 4. Decision ledger

| # | Decision | Rejected alternative | Why |
|---|---|---|---|
| 1 | Runtime reflection over live router | Static AST analysis ("swaggo without comments") | Dynamic registration/loops/middleware wrapping need dataflow analysis; swaggo chose comments to dodge exactly this |
| 2 | Generic return type as metadata carrier | Wrapping handler signatures (Huma-style) | Keeps bodies raw std; factory pattern already exists in the codebase |
| 3 | Go 1.27-only | Baseline 1.22 + runtime assertion fallback | Generic methods make discovery a compile error; no compat baggage internally |
| 4 | One `Responses` slice (status encodes side) | Separate `Responses`+`Errors` | Fewer concepts; merge rules already keyed by status code |
| 5 | Struct literals, no `parts ...any` constructors | `doc.Res(code, typ, opts...)` sugar | No runtime type-switching; everything greppable in one godoc'd struct |
| 6 | Config struct + generic knobs as methods | Functional options | Options can't express `Register[T]`; data knobs want a struct |
| 7 | Responder on deps | `init()` + `SetErrorMapper` global | Explicit wiring, testable with multiple apps |
| 8 | Verb methods `d.Get(r, ...)` | `d.Wrap(r)` decorator + pointer registry | The assignability wall (defined func types) makes true-native impossible; the registry is hidden global state |
| 9 | Build-time walk for chi paths | Trust as-passed patterns | `r.Route`/`r.Mount` compose prefixes invisibly at registration time |
| 10 | `*Generator` implements `http.Handler` | `Mount()` method | Composes with both routers for free |
| 11 | Panic on post-freeze registration | Return error | Same contract as `ServeMux` duplicate-pattern panics |
| 12 | Name `specout` | `oas` (unsearchable), `oasgen`/`livespec`/`codespec` (taken) | Unique search-clean token, self-describing, no stutter |

## 5. Risks & mitigations

- **Deterministic JSON.** Map-based trees marshal with sorted keys in encoding/json (v1 & v2) — component and property order will differ from authoring order. Mitigation: build into ordered structures (small `orderedMap` helper) or canonicalize; golden tests pin bytes from day one (Phase 1).
- **invopop/jsonschema vs json/v2 tags.** Ecosystem maturity for `(omitzero)`/case-sensitive dialect may lag. Mitigation: `schema.go` owns tag reading behind one function; swap invopop's interpretation for our own reader if needed.
- **chi.Walk visibility through user middleware.** Walk reports the registered endpoint handler; our registration keyed the original pointer, so wrapping after registration is safe. If a handler was wrapped *before* being passed in, fall back to the as-passed pattern and surface it in `RequireDocumented` instead of guessing.
- **`r.Pattern` (std) reliance.** Present in modern Go; we're 1.27-only, so no backport concern.
- **Startup cost of reflection.** Built once, lazily; `GO_SPEC_ONLY` artifact is byte-identical if even that matters.

## 6. Acceptance criteria for v0.1

1. Every code sample in `docs/api-reference.html` compiles against the module (verified by `internal/testapp` mirroring them).
2. Quick start serves `/openapi.json` matching its documented YAML excerpt.
3. Upsert (4 response types) and sync (override + `Omit`) fixtures generate exactly as documented.
4. Recorder fails CI on an undeclared status code and on an untested declared one.
5. `RequireDocumented` fails on a plain `r.Get` stray (chi).
6. Two `GO_SPEC_ONLY` runs produce identical bytes.
7. No chi import outside `register_chi.go`, `routes.go`, `recorder/`.
