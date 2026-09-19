# OpenAPI compatibility and release checks

The target is a first **v0.1 release** for Go services that use typed handler
metadata. Output is OpenAPI 3.1 with JSON Schema 2020-12. Go 1.27 or newer is
required. These checks do not establish support for every OpenAPI feature or
every client generator.

The release review fetched `origin/main` at `7b7d262` on 2026-09-19.
The working branch started at that commit. The fixes are on
`feat/openapi-release-checks`.

The confirmed review defects have regression checks. The local release
checks pass, including lint, tests with the race detector, golden comparison,
payload validation, and generated TypeScript type compilation. Linux/amd64
and Windows/amd64 builds also pass. `go mod verify` passes, `go mod tidy -diff`
has no changes, and `govulncheck` reported no known vulnerabilities at review
time. The updated GitHub workflow must still run after these changes are pushed.

This is enough evidence for a scoped v0.1 release. Keep the documented limits
in that release. Broad OpenAPI import or round-trip compatibility has not been
implemented or tested.

## Reproduce the checks

```sh
just build
just lint
just test
just race
just check-golden
just validate
just client-types
go mod verify
go mod tidy -diff
```

`just validate` installs the versions in `tools/requirements.txt` in `.venv`.
It validates the golden document, documents exported by the Go test helpers,
and the demo, Petstore, and Microsoft Graph exports. It also validates union
payloads and compares the public API samples below. Test document export uses
`SPECOUT_VALIDATE_DIR`; it is test code only.

`just client-types` uses Node.js and npm. It runs `openapi-typescript@7.13.0`
and `typescript@7.0.2` on all six examples. A consumer imports each path map and
selects a real operation. It also checks that the Cloudflare success field is
the literal `true`. This checks generated types, not an HTTP request executor.
Temporary files are removed after the run.

CI runs the same recipes, including the race detector. Refreshing public
source files is separate from ordinary CI.

## Public documents tested

The examples are typed reconstructions of selected operations. Specout is an
OpenAPI generator. It does not import or round-trip arbitrary OpenAPI files.

| Source | Selected operations | Comparison scope |
|---|---|---|
| [GitHub](https://github.com/github/rest-api-description/blob/338cb199baa4f326790b0b1c246d8d4f481a82a0/descriptions/api.github.com/api.github.com.json) | List licenses; get repository topics; replace repository topics | Parameters, request bodies, response codes and all selected response schemas |
| [Stripe](https://github.com/stripe/openapi/blob/b21a2a89782c6a964a373574c6c89bda26310870/openapi/spec3.json) | List country specifications; get one country specification | Parameters, request bodies, response codes and success schemas |
| [Cloudflare](https://github.com/cloudflare/api-schemas/blob/1fa08e62b4c342dcf4866346a7c2c0bef940028c/openapi.json) | Verify an API token | Parameters, response codes and success schema |

The source commits, URLs, SHA-256 hashes, and operation list are saved in
[`sources.json`](../examples/compatibility/testdata/sources.json). Run
`just compatibility-refresh` to download those exact versions again, check
their hashes, rebuild the excerpts, and compare the generated contracts.

The comparison ignores descriptions, examples, XML annotations, and vendor
extensions. It resolves local nonrecursive references, normalizes OpenAPI 3.0
nullable types, and merges the simple object `allOf` shapes used by these
samples. It preserves constraint keywords. Cyclic references and unsupported
composition merges fail explicitly. This script is limited to these samples;
it is not a general OpenAPI equivalence checker.

Stripe's large default error graph and Cloudflare's `4XX` error payload are
excluded from payload comparison. Their response codes remain checked.
Security declarations, response headers, links, callbacks, and media encoding
objects are outside this comparison. Stripe's optional empty form request
body uses the existing `Handler.Raw` override.

Petstore and Microsoft Graph have separate served examples and HTTP recorder
tests. They add multipart files, binary responses, OAuth2, several media
types, recursive schemas, and OData parameter names.

## Fixes covered by regression checks

- A route registration keeps its own metadata through router groups and middleware.
- Generic component names are legal and references resolve to the same names.
- Repeated nullable conversion preserves shared schemas and custom schemas.
- Closed schemas retain dictionary values.
- JSON-hidden parameters and promoted parameter fields keep their wire schemas.
- Request body views have names separate from full response schemas.
- Boolean enum tags retain their constraints.
- Path, query, header, and cookie parameters split enum alternatives before nullable conversion.
- Tagged unions constrain the discriminator and payload together.
- Fluent append methods copy their backing slices.
- Raw nested maps produce stable JSON bytes.
- Recorder status checks follow implicit 200, first final status, and flush rules.
- Recorder snapshots do not share mutable status maps with concurrent requests.
- The spec endpoint returns HEAD headers without a body.

## Public API limits

- `Adopt` and `RequireDocumented` scan chi and gorilla routers. Std mux has no
  route enumeration API; direct raw std registrations cannot be checked for completeness.
- `Document` describes other routers manually. The recorder discovers route
  patterns for chi, gorilla, and std mux only.
- The recorder checks declared and observed status codes. It does not validate
  request bodies, response payloads, or headers during a test.
- Typed request bodies are required. Use `Handler.Raw` for an optional body.
- `Config.Auth` entries are alternatives (OR). Use `Raw` for operation scopes
  or combinations that the typed fields cannot express.
- Custom JSON encoders can change a Go type's wire shape. Use `JSONSchema()`
  for those shapes. Schema generation does not execute application encoders.
- There is no OpenAPI 3.0 export mode. Top-level webhooks, path-item reuse,
  shared parameter/response components, and parameter `content` have no
  dedicated typed API.
- `JSONDialect`, `JSONv1`, and `JSONv2` were removed. The option was unused.
  Existing callers that set it must remove that field. Both `omitempty` and
  `omitzero` mark optional schema fields.
- A request type that mixes parameters and body fields now emits its body
  component as `<TypeName>Body`. The full type keeps its own name. Generated
  client type names and golden files can change.
- Anonymous recursive embedding is rejected with a clear panic. Named
  recursive properties remain supported. This avoids a reflector stack overflow.
- Equivalent path templates with different parameter names fail at build time,
  including across methods. Use the same template names for the shared path.

The [OpenAPI 3.1 specification](https://spec.openapis.org/oas/v3.1.1.html)
defines the document and discriminator rules used in these checks. A passing
schema validator alone does not prove correct payloads or working clients.
The separate payload, route, recorder, and client type checks cover those
specific risks.
