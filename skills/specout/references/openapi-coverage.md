# OpenAPI 3.1 coverage

This table lists every field of the
[official OpenAPI 3.1 schema](https://spec.openapis.org/oas/3.1/schema/2025-09-15),
grouped by the schema's own object names. `TestOpenAPICoverage` fails when a
field is missing here, or when a field marked `Yes` or `Raw` does not appear in
its test document.

| Status | Meaning |
|---|---|
| `Yes` | specout has a typed field or generates the value. |
| `Raw` | Write the JSON yourself with the nearest `Raw` field. |
| `In place` | specout writes the object where it is used, so it needs no reusable entry. |
| `No` | specout cannot write this field today. |

The `schema` object is JSON Schema 2020-12. specout builds it from Go types and
`jsonschema` tags. See [types and responses](types-and-responses.md).

## root

| Field | Status | How |
|---|---|---|
| `openapi` | Yes | Always `3.1.0`. |
| `info` | Yes | `Config`. |
| `jsonSchemaDialect` | Raw | `Config.Raw`. |
| `servers` | Yes | `Config.Servers`. |
| `paths` | Yes | Route registration. |
| `webhooks` | Raw | `Config.Raw`. |
| `components` | Yes | Generated. |
| `security` | Yes | `Config.Auth`. |
| `tags` | Yes | `Config.Tags`. |
| `externalDocs` | Yes | `Config.ExternalDocs`. |

## info

| Field | Status | How |
|---|---|---|
| `title` | Yes | `Config.Title`. |
| `summary` | Yes | `Config.Summary`. |
| `description` | Yes | `Config.Description`. |
| `termsOfService` | Yes | `Config.TermsOfService`. |
| `contact` | Yes | `Config.Contact`. |
| `license` | Yes | `Config.License`. |
| `version` | Yes | `Config.Version`. |

## contact

| Field | Status | How |
|---|---|---|
| `name` | Yes | `Contact.Name`. |
| `url` | Yes | `Contact.URL`. |
| `email` | Yes | `Contact.Email`. |

## license

| Field | Status | How |
|---|---|---|
| `name` | Yes | `License.Name`. |
| `identifier` | Yes | `License.Identifier`. |
| `url` | Yes | `License.URL`. |

## server

| Field | Status | How |
|---|---|---|
| `url` | Yes | `Server.URL`. |
| `description` | Yes | `Server.Description`. |
| `variables` | Yes | `Server.Variables`. |

## server-variable

| Field | Status | How |
|---|---|---|
| `enum` | Yes | `ServerVariable.Enum`. |
| `default` | Yes | `ServerVariable.Default`. |
| `description` | Yes | `ServerVariable.Description`. |

## components

| Field | Status | How |
|---|---|---|
| `schemas` | Yes | Generated from Go types. |
| `responses` | In place | Written on each operation. |
| `parameters` | In place | Written on each operation. |
| `examples` | In place | Written on each media type. |
| `requestBodies` | In place | Written on each operation. |
| `headers` | In place | Written on each response. |
| `securitySchemes` | Yes | `Config.Auth`. |
| `links` | In place | Written on each response. |
| `callbacks` | In place | Written on each operation. |
| `pathItems` | In place | Written in `paths`. |

## path-item

| Field | Status | How |
|---|---|---|
| `$ref` | No | |
| `summary` | No | Use `Handler.Summary` on each operation. |
| `description` | No | Use `Handler.Description` on each operation. |
| `servers` | No | Use `Handler.Raw` with `servers` on each operation. |
| `parameters` | In place | Written on each operation. |
| `get` | Yes | Route registration. |
| `put` | Yes | Route registration. |
| `post` | Yes | Route registration. |
| `delete` | Yes | Route registration. |
| `options` | Yes | Route registration. |
| `head` | Yes | Route registration. |
| `patch` | Yes | Route registration. |
| `trace` | Yes | Route registration. |

## operation

| Field | Status | How |
|---|---|---|
| `tags` | Yes | `Handler.Tags`. |
| `summary` | Yes | `Handler.Summary`. |
| `description` | Yes | `Handler.Description`. |
| `externalDocs` | Yes | `Handler.ExternalDocs`. |
| `operationId` | Yes | `Handler.OperationID`, or generated. |
| `parameters` | Yes | The `Req` type. |
| `requestBody` | Yes | The `Req` type. |
| `responses` | Yes | The `Res` type and `Handler.Responses`. |
| `callbacks` | Raw | `Handler.Raw`. |
| `deprecated` | Yes | `Handler.Deprecated`. |
| `security` | Yes | `Handler.Security` or `Handler.Public`. |
| `servers` | Raw | `Handler.Raw`. |

## external-documentation

| Field | Status | How |
|---|---|---|
| `description` | Yes | `ExternalDocs.Description`. |
| `url` | Yes | `ExternalDocs.URL`. |

## parameter

| Field | Status | How |
|---|---|---|
| `name` | Yes | The `path`, `query`, `header`, or `cookie` tag. |
| `in` | Yes | The `path`, `query`, `header`, or `cookie` tag. |
| `description` | Yes | `jsonschema:"description=..."`. |
| `required` | Yes | `omitempty`, `omitzero`, or a default make it optional. |
| `deprecated` | Yes | `jsonschema:"deprecated"`. |
| `schema` | Yes | The field type and `jsonschema` tag. |
| `content` | No | |
| `allowEmptyValue` | Yes | `jsonschema:"allowEmptyValue=true"`. |
| `style` | Yes | `jsonschema:"style=..."`. |
| `explode` | Yes | `jsonschema:"explode=true"`. |
| `allowReserved` | Yes | `jsonschema:"allowReserved=true"`. |

## request-body

| Field | Status | How |
|---|---|---|
| `description` | Yes | `Handler.RequestDescription`. |
| `content` | Yes | The `Req` type and `Handler.RequestContentTypes`. |
| `required` | Yes | Required unless `Handler.RequestOptional` is set. |

## media-type

| Field | Status | How |
|---|---|---|
| `schema` | Yes | Generated from Go types. |
| `encoding` | Raw | `Handler.Raw` with the whole `requestBody`. |

## encoding

| Field | Status | How |
|---|---|---|
| `contentType` | Raw | `Handler.Raw` with the whole `requestBody`. |
| `headers` | Raw | `Handler.Raw` with the whole `requestBody`. |
| `style` | Raw | `Handler.Raw` with the whole `requestBody`. |
| `explode` | Raw | `Handler.Raw` with the whole `requestBody`. |
| `allowReserved` | Raw | `Handler.Raw` with the whole `requestBody`. |

## responses

| Field | Status | How |
|---|---|---|
| `default` | Yes | `Response.Status` 0 or `Response.Key`. |

## response

| Field | Status | How |
|---|---|---|
| `description` | Yes | `Response.Description`, or the status text. |
| `headers` | Yes | `Response.Headers`. |
| `content` | Yes | `Response.Type`, `ContentType`, or `ContentTypes`. |
| `links` | Raw | `Response.Raw`. |

## example

| Field | Status | How |
|---|---|---|
| `summary` | Yes | `Example.Summary`. |
| `description` | Yes | `Example.Description`. |
| `value` | Yes | `Example.Value`. |
| `externalValue` | Yes | `Example.ExternalValue`. |

## examples

| Field | Status | How |
|---|---|---|
| `example` | Yes | `Response.Example` or `Handler.RequestExample`. |
| `examples` | Yes | `Response.Examples` or `Handler.RequestExamples`. |

## link

| Field | Status | How |
|---|---|---|
| `operationRef` | Raw | `Response.Raw`. |
| `operationId` | Raw | `Response.Raw`. |
| `parameters` | Raw | `Response.Raw`. |
| `requestBody` | Raw | `Response.Raw`. |
| `description` | Raw | `Response.Raw`. |
| `server` | Raw | `Response.Raw`. |

## header

| Field | Status | How |
|---|---|---|
| `description` | Yes | `Header.Description`. |
| `required` | Yes | `Header.Required`. |
| `deprecated` | Yes | `Header.Deprecated`. |
| `schema` | Yes | `Header.Type`. |
| `content` | No | |
| `style` | No | |
| `explode` | No | |

## tag

| Field | Status | How |
|---|---|---|
| `name` | Yes | `Tag.Name`. |
| `description` | Yes | `Tag.Description`. |
| `externalDocs` | Yes | `Tag.ExternalDocs`. |

## reference

| Field | Status | How |
|---|---|---|
| `$ref` | In place | specout references schemas only. |
| `summary` | In place | specout references schemas only. |
| `description` | In place | specout references schemas only. |

## security-scheme

| Field | Status | How |
|---|---|---|
| `type` | Yes | `AuthScheme.Type`. |
| `description` | Yes | `AuthScheme.Description`. |
| `name` | Yes | `specout.APIKey`. |
| `in` | Yes | `specout.APIKey`. |
| `scheme` | Yes | `specout.Bearer`, `specout.Basic`, or `AuthScheme.Scheme`. |
| `bearerFormat` | Yes | `AuthScheme.BearerFormat`. |
| `flows` | Yes | `specout.OAuth2`. |
| `openIdConnectUrl` | Yes | `AuthScheme.URL`. |

## oauth-flows

| Field | Status | How |
|---|---|---|
| `implicit` | Yes | The `implicit` key of `Flows`. |
| `password` | Yes | The `password` key of `Flows`. |
| `clientCredentials` | Yes | The `clientCredentials` key of `Flows`. |
| `authorizationCode` | Yes | The `authorizationCode` key of `Flows`. |
| `authorizationUrl` | Yes | `OAuth2Flow.AuthorizationURL`. |
| `tokenUrl` | Yes | `OAuth2Flow.TokenURL`. |
| `refreshUrl` | Yes | `OAuth2Flow.RefreshURL`. |
| `scopes` | Yes | `OAuth2Flow.Scopes`. |
