# Import, Export, and Code Generation

English | [简体中文](../interop.md)

Related: [Documentation Index](./index.md) · [Data Model](./data-model.md) · [Extensibility](./extensibility.md)

All converters follow `external format -> internal model (IR) -> external format`. The IR is the [shared type contract](./data-model.md#3-shared-frontendbackend-types-contract).

---

## 1. Overview

Use a common Adapter interface for every converter:

- **Import**: Postman v2.1, OpenAPI 3.x / Swagger 2, cURL commands, HAR, and Insomnia.
- **Export**: Postman v2.1, OpenAPI, cURL, and code snippets.

Generate HTTP code from the internal model for cURL, JavaScript (fetch/axios), Python (requests), Go, Java, Rust, PHP, and other targets. Extend the matrix through templates and a generator interface.

---

## 2. Field-Level Import and Export Mappings

These mappings cover the key formats so implementation does not repeatedly depend on external documentation.

### 2.1 Postman Collection v2.1 <-> IR

| Postman v2.1 | Internal Model | Notes |
|--------------|----------------|-------|
| `info.name` / `info._postman_id` | collection node `name` / `id` | Regenerate conflicting IDs and retain a mapping |
| `item[]` including nested `item` | `node` tree (folder/request) | Recursive; an item with a `request` field is a request |
| `request.method` / `request.url.raw` | `method` / `url` | URL may be an object and must be assembled into `raw` |
| `request.header[]` | `headers: KV[]` | `disabled` -> `enabled=false` |
| `request.url.query[]` | `params: KV[]` | Deduplicate and merge with query parameters in the URL |
| `request.body.mode` | `body.kind` | Direct mapping for raw/urlencoded/formdata/file/graphql |
| `request.body.raw` + `options.raw.language` | `body.text` + `language` | Infer a missing language from the header |
| `request.auth` | `auth` | Align type names and convert the parameter array into an object |
| `event[]` (prerequest/test) | `pre_script` / `test_script` | Use `listen` to select the phase and join `script.exec[]` into text |
| collection/folder `variable[]` | node `variables` | |
| `{{var}}` placeholder | Preserve verbatim | Syntax is already compatible |

Export applies the reverse mapping. If IR fields such as per-request SSL overrides cannot be represented, degrade to the closest semantics or add a note to `description`.

### 2.2 OpenAPI 3.x / Swagger 2 -> IR (Import Only)

| OpenAPI | Internal Model |
|---------|----------------|
| `info.title` | collection name |
| `tags` | top-level folder groups |
| `paths./x.{method}` | request, named from `operationId` or `summary` |
| `servers[0].url` + path | URL, converting server variables to `{{var}}` |
| `parameters(in=query/header/path)` | params / headers / path-segment placeholders |
| `requestBody.content` | body, selecting raw/formdata/urlencoded by media type |
| `security` + `securitySchemes` | auth mapping for bearer/apiKey/oauth2 |
| `components.examples` / `responses` | Save as Examples for Mock Server use |

- **`$ref` resolution**: fully dereference local, relative, and remote references before conversion.
- **`oneOf`/`anyOf`/`allOf`**: generate a sample body from the first usable schema.
- Convert server variables and path parameters into environment variables, then prompt the user to provide values after import.

### 2.3 cURL -> IR (Command-Line Parsing)

Parse `-X/--request`, `-H/--header`, `-d/--data*`, `-F/--form`, `-u/--user`, `--url`, `-b/--cookie`, `--compressed`, `-k/--insecure`, and related options:

- Merge repeated `-d` arguments. If `Content-Type` is JSON, use `body.kind=raw(json)`; otherwise use urlencoded.
- Convert `-F` to formdata and recognize an `@file` prefix as a file item.
- Convert `-u user:pass` to Basic auth.
- Reuse the code generator's cURL target for the reverse IR -> cURL conversion.

### 2.4 HAR -> IR

Convert each `log.entries[].request` into a request using method, URL, headers, queryString, and postData. Group requests sharing a host into one folder. Optionally save each `response` as an Example for traffic-replay workflows.

---

## 3. Code Generator Architecture

`IR(HttpRequest, resolved variables?) -> target-language snippet`. Register generators by the `(language, client)` pair behind a common interface:

```go
type CodeGen interface {
    Id() string                                          // "javascript-fetch" / "python-requests" ...
    Generate(req *HttpRequest, opts *GenOptions) string
}
```

- **Target matrix**: curl, JavaScript (fetch/axios), Python (requests/http.client), Go (net/http), Java (OkHttp/HttpClient), Rust (reqwest), PHP (curl/Guzzle), C# (HttpClient), Node (native), and Shell (httpie).
- **GenOptions**: preserve `{{var}}` placeholders or inline resolved values, indentation style, include auth, and include comments.
- **Correctness**: quoting, multiline bodies, and binary data require different escaping in each language. Keep golden tests per target to prevent generator drift.
- Keep code generation separate from export: generation produces copyable snippets, export produces exchange files, but both share the same IR.
