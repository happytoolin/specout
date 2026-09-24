Launch review — 24 September 2026

All confirmed defects from these reviews are fixed or rejected with a clear
input error. The latest pass tested unusual routes, malformed request bodies,
and conflicting schema tags. No confirmed launch blocker remains from these checks.

The review covers changes after commit `033c33a`.
No dependencies were added or updated.

| Earlier finding | Fix | Regression coverage |
| --- | --- | --- |
| Generated union branches collide with user types | Finish reflection before choosing branch names. Reserve names used by nested types too. | `TestLaunchUnionNameCollision`, `TestLaunchUnionNameCollisionWithNestedComponent` |
| Raw responses bypass status verification | Derive status coverage from the effective raw response keys, including ranges and defaults. | `TestLaunchRawResponsesCannotBypassVerification`, `TestLaunchRawStatusRangesAndDefaults` |
| Root pointers lose nullability | Allow null at each request or response use without making the shared component nullable. | `TestLaunchRootPointerNullability`, `TestLaunchPointerContainersAndSplitBodies` |
| Form tags change JSON field names | Apply form names only to form request schemas. Keep JSON schemas separate. | `TestLaunchJSONNamesIgnoreFormTags`, `TestFormRenamesPreserveSharedFields`, petstore upload test |
| Nil embedded pointers require absent child fields | Track pointer ancestors when flattening embedded fields. | `TestLaunchNilEmbeddedPointer`, split-body payload cases |
| Nullable wrappers stop nested fixes | Walk the non-null composition branches before applying field and container fixes. | `TestLaunchNullableWrapperFixups`, nullable container payload cases |
| Excluded recursive fields cause a panic | Check only fields visible to schema reflection. | `TestLaunchExcludedRecursiveField`, `TestLaunchIgnoredRecursiveFields` |
| chi HEAD fallback and path rewrites hide response drift | Retain the routing context and record the final method and route pattern. | HEAD coverage, undeclared HEAD 500, StripSlashes, mounted rewrite and explicit HEAD tests |
| Onboarding errors return plain text | Return the declared JSON problem body for 400, 401, and 404 responses. | `TestLaunchErrorBodiesMatchContract` |
| Recursive pointer types hang generation | Detect pointer cycles and reject them with a clear specout panic. | `TestLaunchRecursivePointerTerminates`, isolated request and response processes with time limits |
| Repeated boolean enum values are lost | Read all enum entries and remove duplicate values. | `TestLaunchRepeatedBooleanEnums` |
| Excluded embedded schemas return to the output | Honor schema exclusion during field promotion. | `TestLaunchExcludedEmbeddedSchema` |
| A literal JSON dash field disappears | Distinguish the exact exclusion tag from a literal dash name. Preserve field options and explicit required tags. | Saved JSON-name fuzz input and required-field tests |
| A literal asterisk route disappears | Use each router's wildcard rules. Standard and gorilla routes retain literal stars. | `TestLaunchStdLiteralAsteriskIsDocumented` |
| Invalid onboarding stages are stored | Validate the stage before changing the store. | `TestLaunchOnboardingRejectsInvalidStage` |

The further bug check found these defects:

| Finding | Evidence before the fix | Result after the fix |
| --- | --- | --- |
| Unmatched chi subroutes are recorded as parent operations | Requests to a missing child route or unsupported method reported drift against `/api/*`. | Only endpoint matches are recorded. The mounted-route regression passes. |
| Nullable boolean enums reject null | `jsonschema:"nullable,enum=true,enum=false"` rejected the explicit null value allowed by the tag. | Apply enum values to the non-null branch. Positive and negative payload cases pass. |
| Inline unions remain invalid in forms and parameters | Form bodies and query objects used a combined variant name as a JSON Schema type. | Normalize inline schemas too. Renamed form discriminator and payload fields retain the union constraint. Valid payloads pass; mismatches fail. |

The unusual-input pass found these additional failures:

| Input or trigger | Failure before the change | Current behavior |
| --- | --- | --- |
| Unicode route segments, including Japanese text and emoji | Operation-ID generation split a UTF-8 character. The entire document failed to encode. | Capitalize a complete rune. Literal Unicode routes generate valid documents. |
| A valid request object followed by another value, junk, or a NUL byte | The decoder accepted the first object. Onboarding changed stored data despite the malformed body. The other examples used the same decode pattern. | All three examples require exactly one complete JSON value before taking an action. |
| Whitespace-only bearer tokens or API keys | The onboarding example allowed a protected DELETE with no real credential value. | Return the declared JSON 401 response. This remains example credential checking, not token verification. |
| Invalid owner addresses such as `@` and `a@@example.com` | Onboarding stored and returned them as valid email addresses. | Use the standard email parser and require one plain address before changing data. |
| Repeated union variants | Identical oneOf branches caused valid payloads to fail. | Remove duplicate variant names while keeping declaration order. |
| Repeated enum tags combined with pipe-separated values | Valid individual values failed, while an unsplit value was accepted. | Expand every tagged value and remove duplicate string values. |
| An excluded parameter schema with a description | Generation dereferenced a nil schema. Without a description, it could emit a null schema. | Reject it with a clear error naming the field. |
| Malformed, quoted JSON field-name tags | The document named fields that did not match the JSON encoder. | Reject the malformed name with a clear error. |
| Explicit embedding of a named field with `inline` or `embed` | The reflector and the installed JSON encoder produced different object shapes. | Reject this unsupported combination. Anonymous struct embedding remains supported. |

This pass added 12 regression test functions and two fuzz targets. It also
checks that rejected onboarding bodies leave existing store values unchanged.
The petstore and Graph tests check that malformed bodies cannot reach their
action or return a successful decode.

The JSON parser fuzz test uses raw JSON values as its target. A very large
valid number is retained as a seed; it must not be confused with float64
conversion limits. Quoted numeric enums remain a passing control.

Test coverage added across all passes:

- 41 launch test functions and five fuzz targets.
- A saved, minimized fuzz input for the literal JSON dash field.
- Positive and negative JSON Schema payload checks.
- Checks for failed-build recovery and concurrent exports.
- A corrected test helper that does not mistake an ordinary two-arm union for a nullable schema.
- `just adversarial`, included in `just check` and therefore in CI and release checks.

The existing form tests now select form media explicitly. The petstore upload
test checks the actual multipart request schema. These changes keep the tests
aligned with the wire format instead of requiring form names in JSON components.

Final validation:

| Check | Result |
| --- | --- |
| `just check` | Passed all steps |
| Module integrity, build, formatting, lint, workflow lint | Passed; zero lint issues |
| Unit tests and race detector | Passed; no race reports |
| OpenAPI and JSON Schema suite | Passed for 209 generated test documents and three served examples |
| Adversarial payload runner | Passed for 66 documents and 39 serialized payload cases |
| Generated TypeScript clients | Generated and compiled for all three examples |
| `govulncheck -test ./...` | No vulnerabilities found |
| JSON field-name fuzzing | 450,252 executions; passed |
| Escaped literal-path fuzzing | 932,512 executions; passed |
| Response-plan parity fuzzing | 320,280 executions; passed |
| Literal Unicode path fuzzing, latest pass | 1,035,443 executions; passed |
| Complete JSON value fuzzing, latest pass | 41,430 executions in a timed run, then 100,000 executions in a fixed-count run; passed |

The earlier three fuzz runs completed 1,703,044 executions. The latest two
targets completed another 1,176,873 executions after corrections. The timed
runs used 30 seconds and two workers each. The JSON parser also passed a
100,000-execution run. Ordinary tests replay the saved fuzz inputs.

The following simplification passes kept the public API unchanged. They
removed duplicate name tracking, stored flags, and the request-body type cache.
All parameter locations now use one builder. Union handling and inline schema
reflection have separate files. `CONTRIBUTING.md` explains the build flow.
The 208 existing test documents retain the same JSON content. One additional
document tests parameter requirements, nullability, styles, and descriptions.

Recursive pointer-only types remain unsupported. They now fail immediately
instead of hanging. Explicit JSON embedding on a named field is also rejected;
use an anonymous struct field for supported promotion. This review is a finite test of the repository and its
examples, not proof that every application input is correct.

To repeat the full check:

```sh
just check
```

To repeat the launch tests and payload checks:

```sh
just adversarial
```

To continue fuzzing:

```sh
go test -run '^$' -fuzz '^FuzzLaunchJSONFieldNames$' -fuzztime=60s -parallel=2 .
go test -run '^$' -fuzz '^FuzzLaunchStdLiteralPaths$' -fuzztime=60s -parallel=2 .
go test -run '^$' -fuzz '^FuzzLaunchResponsePlanParity$' -fuzztime=60s -parallel=2 .
```

Run the new fuzz targets directly:

```sh
go test -run '^$' -fuzz '^FuzzLaunchUnicodeLiteralPaths$' -fuzztime=60s -parallel=2 .
go test -run '^$' -fuzz '^FuzzLaunchDecodeSingleJSONValue$' -fuzztime=100000x -parallel=2 ./internal/examplekit
```
