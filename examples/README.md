# Examples

Two published OpenAPI documents, rebuilt with specout and served. Nothing here
is invented: paths, operationIds, parameter names, response codes and response
descriptions are the published ones. Only the descriptions are shortened, and
the parts specout cannot say are listed under Gaps below.

| Example | Document | Router | Port |
|---|---|---|---|
| `petstore` | petstore3.swagger.io/v3/openapi.json, OpenAPI 3.0.4, 13 paths, 19 operations | chi | 8081 |
| `msgraph` | Microsoft Graph v1.0, OpenAPI 3.0, 11546 paths - 5 paths, 8 operations | gorilla/mux | 8082 |

Run one:

```sh
go run ./examples/petstore   # swagger ui at http://localhost:8081/
go run ./examples/msgraph    # swagger ui at http://localhost:8082/
```

Each serves Swagger UI at `/` and the generated document at its
`openapi.json`. `GO_SPEC_ONLY=1 go run ./examples/petstore` writes the
document to stdout; the tests use that path.

The tests are the point of the examples. `go test ./examples/...` builds the
document, checks it against the published facts, and then drives every
operation over real HTTP through `recorder.Verify`. A handler that writes a
status the document does not declare fails the test.

## Gaps

What the four documents use most, and specout has no way to say. Counts come
from a scan of the published files: petstore 3.0.4, Stripe `spec3.json`,
GitHub `api.github.com.json`, Microsoft Graph v1.0.

| Published shape | In the real documents | In specout |
|---|---|---|
| `4XX` / `5XX` / `2XX` range response keys | Graph 17870 / 17870 / 14456 | Status keys are integers. `Config.DefaultErrors []int` names the codes a range covers instead - 400, 401, 403, 404, 429, 500 in the msgraph example. |
| Two content types on one response | petstore 11, GitHub 5, Graph 14 | One `Response` is one content type. The JSON form is carried, the XML form is dropped. |
| `application/x-www-form-urlencoded` and XML bodies | petstore 5 and 5, Stripe 593 | `application/json`, `multipart/form-data` (a `File` field) and `application/octet-stream` (a bare `File`) only. |
| `xml` on a schema | petstore 9 | No. |
| `oauth2` schemes, flows and scopes | petstore `petstore_auth`, implicit | `specout.Bearer`, `specout.APIKey` and `openIdConnect` only. An `oauth2` AuthScheme panics. The petstore example declares the published `api_key` scheme and notes the one it cannot. |
| `$ref` to `components.parameters` / `responses` / `requestBodies` / `examples` / `headers` | GitHub 3175 + 2058, Graph 17219 + 40179 | Every parameter, response and body is inlined at its operation. The document is longer, and self-contained. |
| `style` and `explode` on a parameter | Stripe 354 `deepObject`, 608 `form`, 440 `simple`; Graph 14788 `form` | Not emitted. An object-valued query parameter renders as a JSON object, with no way to ask for `deepObject`. |
| `content` on a parameter | Graph and GitHub use it | No. Query parameters are tags on the Req type. |
| `externalDocs` on an operation or a tag | GitHub 1239, Graph 3633, petstore 2 tags | `Config.ExternalDocs` only, so document level. |
| `info.contact`, `info.license`, `info.termsOfService` | all four | No. `Config` has title, version, description, servers, auth, tags and externalDocs. |
| `components.examples`, and `examples` on a schema | GitHub 535, Graph 3034 | The `example=` tag gives one example on a property or a parameter. |
| `deprecated` on a schema property | GitHub 32, Stripe 2 | `Handler.Deprecated` is operation level only. |
| `x-` vendor extensions on schemas and path items | Graph 3486, Stripe 2669, GitHub 53 | `Response.Raw` splices arbitrary keys into a response object. Nothing reaches a schema or a path item. |
| `anyOf`, and `allOf` inheritance | Stripe 2051, Graph 3976 and 3742, GitHub 35 and 78 | `Register[T]` plus a `oneof_type` field gives `oneOf` with a discriminator - the shape Graph (277) and GitHub (5) use. `anyOf` and `allOf` have no form: inheritance is flattened into one Go struct, as `User` in the msgraph example shows. |
| `webhooks`, `components.pathItems` | 3.1 features, absent from these four | No. |

Two shapes that look like gaps and are not:

- `nullable` (Stripe 2727, Graph 11025, GitHub 3994). A Go pointer field
  emits `type: ["string", "null"]`, the OpenAPI 3.1 form. The 3.0
  documents say `nullable: true`; the 3.1 document specout writes says
  the same thing as a type union.
- `default`, `pattern`, `minLength`, `minItems`,
  `uniqueItems`, `enum`, `format`, `minimum`,
  `example` (Graph 14788 `uniqueItems` on its OData array
  parameters). All ride the `jsonschema` struct tag, and reach both body
  properties and parameters.

## Shapes these examples carry over

Checked by the tests, not by eye:

- typed path parameters with descriptions, including a hyphenated name
  (`user-id`) and three parameters in one segment,
  `getPolicyId(type='{type}',name='{name}')`;
- query, header and cookie parameters; array query parameters; an object query
  parameter as `additionalProperties`;
- parameter metadata: `format`, `enum`, `default`,
  `minimum`, `example`;
- response headers, binary responses, body-less 204s, `default`
  responses;
- one global error envelope (`ErrorType`) declared per operation by code,
  and the per-operation `security: []` opt-out;
- multipart file upload, and a `Req` that mixes path parameters with a
  body that is split into its own component (Graph `sendMail`);
- hyphenated and `@`-prefixed JSON property names, inlined maps, enums,
  `enum=text|html`, nested and recursive schemas.

## Deliberate deviations

- Descriptions are the published first sentences. `specout.Raw` carries
  the ones the generator's own wording would overwrite.
- Microsoft Graph keys its failures by range; the example lists the codes.
  Graph also declares no `securitySchemes`, so the example declares none.
- Stripe, GitHub and Microsoft Graph use `$ref` for parameters,
  responses and examples; these examples inline, which is what specout emits.
- `allOf` inheritance (Graph `user` extends `directoryObject`)
  is flattened into one struct.

## Router notes

- `Adopt()` is required for chi and gorilla. It walks the live router, so
  prefixes composed by `r.Route`, `r.Mount` or `PathPrefix`
  resolve. A route the walk never sees is a build error naming the pattern, not
  a `""` path. `specout.Std` needs no walk: its patterns are already
  absolute.
- Document order is registration order. The `paths` object reads like the
  registration code.
- `Req` is path, query and body together. specout splits the parameter
  fields out, so a path parameter never appears as a body property.
- A group root becomes `/pet/`. A route registered as `"/"` inside a
  group on `/pet` is documented and served as `/pet/`. chi answers a
  request to `/pet` as well; gorilla answers 404.
