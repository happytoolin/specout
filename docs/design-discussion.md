# specout — design discussion log

Condensed, faithful capture of the design conversation that produced this package
(2026-09-15). Each entry: the question asked → options considered → what was decided.
The full public API surface lives in [`api-reference.html`](api-reference.html);
implementation order in [`../PLAN.md`](../PLAN.md).

---

### 1. "A simpler OpenAPI generator from Go code, std/chi routers — I dislike swaggo's comments"

Options laid out:
- **Runtime walk + reflection** — chi's `Walk` enumerates the live router; std mux has no
  public enumeration (needs a registration shim); schemas via reflection (`invopop/jsonschema`);
  spec served from the same binary, so routes can't drift.
- **Static AST analysis** — `go/packages` finding `r.Get("/x", h)` calls. Rejected as a
  foundation: dynamic registration, loops, middleware wrapping require dataflow analysis;
  this is the swamp swaggo dodged by using comments.
- **Code-as-data / typed handlers** (Huma model) — most metadata-dense, but replaces the
  handlers rather than documenting them.

**Decision:** hybrid — runtime walk + reflection, metadata co-located with handler factories.
Key framing: routes and schemas can be inferred; descriptions/status codes never can — the
design question is only where that residue lives (tags, tiny builder calls, or convention).

### 2. "What about the factory pattern — generics on the outer wrapper?"

User showed `func HandleDeleteOnboarding(deps) http.HandlerFunc { ... return func(w, r) {} }`.

**Decision:** the return type becomes the metadata carrier:
`specout.Handler[Req, Res]` embedding `http.HandlerFunc` (closure stays raw std), with
`Types() (req, res reflect.Type)` as the reification point discovered via a `Documented`
interface. Caveats accepted: types are advisory not enforced (mitigated by write-helpers),
and plain handlers silently skip documentation (mitigated later by a completeness test).

### 3. "How do 200/201/400 etc. work?"

**Decision:** three layers that agree by convention:
global error envelope (RFC 9457 Problem, stamped on default codes), explicit success-side
declarations (`Res` → 200, `NoContent` → 204, extra codes in a slice), and a **recorder**
wrapping `http.ResponseWriter` during integration tests that diffs declared vs observed
status codes — turning the test suite into spec verification, which no offline generator can do.

### 4. "What if one API has 3/4 response types?"

**Decision:** declarations are linear data — entries carry `(status, type)`; same type at
two codes dedupes into `components` and is `$ref`'d everywhere; write-helpers
(`OK`/`Status`/`Err`) stay orthogonal to declarations; the recorder ensures every declared
code is actually exercised. Rule of thumb: past ~4 entries is an endpoint design smell.

### 5. "What about tags / everything latest OpenAPI supports?"

**Decision:** every feature has exactly one home, chosen by scope — field-level → struct tags
(`json`, `jsonschema`, `query`), operation-level → Handler fields, API-level → setup config.
Target **OpenAPI 3.1** (real JSON Schema 2020-12, no translation layer). The three-dials
tripwire documented (required ← `omitempty`, nullable ← pointer, default ← tag). Unions need
explicit registration (Go has no sum types). Escape hatches guarantee "never blocked":
`JSONSchema()` on types, raw JSON merge anywhere.

### 6. HTML reference built

Single self-contained `api-reference.html` written and browser-verified (syntax highlighting,
layout, mobile). Maintained alongside the design from here on.

### 7. "I don't like the global-only error envelope — I want per-route shapes"

**Decision:** the global envelope became the floor, not the ceiling: per-route error
declarations can give any status code a completely different shape or remove a code a route
can't produce (`Omit`); `DetailedError` (`HTTPStatus() int`, `Payload() any`) lets domain
errors carry their wire shape through the single `Err` exit, with the generic mapper as
fallback. Merge rules: unmentioned codes keep the global shape; overrides are per-route only.

### 8. "Any Go 1.27 features? I hear better generics"

Research: 1.26 brought self-referential type params; 1.27 brought **generic methods**,
`encoding/json/v2`, stdlib `uuid`, improved inference. **Decision:** target 1.27-only.
Generic methods turn registration into compile-checked discovery (`d.Get(r, path, h)` —
a plain `http.HandlerFunc` fails to compile). json/v2 becomes the default tag dialect
(`(omitzero)`), v1 opt-in. Interface methods still can't be generic — hence methods on the
generator taking the router as an argument.

### 9. "Any more idiomatic API suggestions?"

Honest idiom review. **Decisions adopted:**
- functional options → **one Config struct**; generic knobs (`Register[T]`, `SchemaName[T]`)
  become methods (type params can't be struct fields)
- `parts ...any` type-switching constructors → **plain `Response` struct literals**
  (`{Status, Type, Headers, Raw, Omit}`)
- **one `Responses` slice** (the status code already encodes success/error side)
- `init()` + `SetErrorMapper` global → **responder wired through deps**
  (`api.New(api.Config{ErrorMapper})`, called as `deps.API.Err/OK/Status`)
- `*Generator` implements `http.Handler` (`r.Mount("/openapi.json", d)` / std `mux.Handle`)
  with stdlib-style lifecycle: lazy build, freeze, panic on late registration
- recorder → `specout/recorder` subpackage; `MountChi` → `Adopt`
- rejected: `d.Wrap(r)` decorator + global pointer registry (hidden global state)

### 10. "Route grouping — as native as possible"

**Decision:** grouping/middleware/mounting stay 100% native (`r.Route`, `r.Use`, `r.Mount`).
The per-route verb call is the one irreducible custom bit — the **assignability wall**:
chi's verb methods take the defined type `http.HandlerFunc`, a metadata-carrying handler is
not assignable to it, and interface methods can't take type parameters. So: verb methods
mirroring chi 1:1 (`d.Get(r, "/", h)` … `d.Trace`). Path subtlety found and fixed:
registration records only handler metadata; **full paths resolve at build time via a single
`chi.Walk`** (Route/Mount prefixes are invisible at registration time). Spec-side grouping =
tags; optional `DeriveTags` heuristic noted.

### 11. "What about gin/echo/fiber/fiberv3?"

**Decision:** tiered. Core kept router-agnostic; gin/echo adapters are cheap later (they
even expose native route enumeration — `Routes()` — and serve net/http, so the recorder
carries over); fiber (v2/v3, fasthttp) means a parallel decode/respond/recorder surface —
only if a real service demands it. **Scope for v0: std + chi, both blessed, chi code confined
to registration/enumeration paths.** Known asymmetry accepted: the completeness test can
catch stray plain-handler routes on chi (walkable) but not on std (no enumeration).

### 12. "What should we name it? Check SEO"

Search-checked candidates: `oasgen` ❌ (Rust library doing the same thing),
`livespec` ❌ (AI spec tool + wallpaper app), `codespec` ❌ (Rust crate), `oas` ⚠️
(unsearchable token). Clean: `specout`, `honestapi`.

**Decision: `specout`** — "it specs out your handlers"; unique search-clean token,
self-describing, no stutter (`*Generator`), natural CI verb ("CI specouts the build"),
subpackages read well (`specout/recorder`, future `specout/chi`). Tagline carries the
keywords: *OpenAPI 3.1 from Go handlers. No comments. Verified by your own tests.*

---

## Artifact provenance

- `docs/api-reference.html` — the full public API reference (18 sections, tag tables,
  recipes, FAQ, godoc-style index), renamed to specout in the same pass as this log.
- `PLAN.md` — phased implementation plan, decision ledger, risks, v0.1 acceptance criteria.
