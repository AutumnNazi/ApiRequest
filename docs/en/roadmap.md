# Feature Checklist, Roadmap, and Risks

English | [简体中文](../roadmap.md)

> Full feature checklist, phased implementation plan, key technical risks, and a ready-to-implement Phase 1 task breakdown.
> Back to the [Documentation Index](./index.md).

---

## 1. Full Feature Checklist

### Request Construction

- [x] Complete HTTP method set (GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS + custom)
- [ ] URL and Query parameter editing with bidirectional synchronization
- [ ] Header editing with autofill, bulk editing, and disabled rows
- [x] Body: form-data / x-www-form-urlencoded / raw (JSON/XML/HTML/Text) / binary / GraphQL
- [x] Auth: No Auth / Basic / Bearer / API Key / Digest / OAuth 1.0 / OAuth 2.0 / AWS Signature
- [x] Per-request overrides for timeout, redirects, and SSL verification

### Variables and Environments

- [x] Global / collection / environment / local variable scopes
- [x] Environment switching and quick editing
- [x] Dynamic variables such as `{{$guid}}`, `{{$timestamp}}`, and `{{$randomInt}}`
- [x] Variable-resolution preview and undefined-variable warnings

### Scripts

- [x] Pre-request scripts
- [x] Test scripts and assertions
- [x] Collection/folder script inheritance
- [x] Compatible subset of the `pm.*` API
- [x] `sendRequest` inside scripts

### Responses

- [x] Body views: Pretty (JSON) / Raw / Preview (HTML/SVG) + text search highlighting
- [x] Headers / Cookies / test-results tabs
- [x] Status code, duration, size, and phase timings
- [x] Streaming large responses with blob persistence, preview excerpts, and on-demand full loading
- [x] Save as Example

### Organization and Management

- [x] Workspaces with switch/create/rename/delete
- [x] Collection / nested folder / request tree with recursive nesting, expand/collapse, and double-click rename
- [x] Multi-tab editing
- [x] Searchable, replayable, clearable history
- [x] Cookie manager for viewing and editing the Cookie Jar

### Protocol Extensions

- [x] WebSocket
- [x] Server-Sent Events (SSE)
- [x] GraphQL schema introspection and completion (backend introspection; GraphQL body type supported)
- [x] gRPC discovery through server reflection and dynamic invocation with `dynamicpb`, including unary, client/server streaming, and bidi

### Interoperability

- [x] Import: Postman v2.1 / OpenAPI 3.x / Swagger 2 / cURL / HAR / Insomnia
- [x] Export: Postman v2.1 / OpenAPI 3.0.3 / cURL as JSON + shell script
- [x] Code generation: cURL / JS fetch / Python requests / Go / Java / Rust / PHP / C#
- [x] Git-friendly collection mirror using a JSON directory tree with one file per request

### Advanced

- [x] Mock Server
- [x] Collection Runner with data-file input and run reports
- [x] Proxy settings for system/manual/direct modes
- [x] Custom TLS and client certificates with custom-CA trust and mTLS
- [x] Team synchronization over WebDAV with user-provided Nutstore, Nextcloud, or another server; snapshots + entity-level LWW merge; secrets may be omitted. See [sync.md](./sync.md).

---

## 2. Current Status and Next Work

Phases 1 through 5 are implemented in the current `dev` line: the native request engine, scoped variables and scripts, authentication and converters, Runner/Mock/protocol extensions, WebDAV synchronization, gRPC, the CLI runner, and the theme/accessibility foundations are all covered by backend or frontend tests.

The remaining work is deliberately incremental rather than a second rewrite:

- Improve URL/query and header bulk-edit ergonomics while preserving ordered rows and disabled entries.
- Add a virtualized history viewport for very large local databases.
- Expand end-to-end desktop smoke coverage for native dialogs, keychain fallback, signed artifacts, and update-manifest consumption.
- Define a signed updater protocol and rollback policy before enabling in-app binary replacement. The current Settings action opens the verified GitHub release page; it never replaces binaries silently.

---

## 3. Key Technical Risks

| Risk | Details | Mitigation |
|------|---------|------------|
| Script compatibility | The Postman `pm.*` API is broad and full compatibility is expensive | Cover the high-frequency subset first, expand as needed, and document support |
| Large-response performance | Rendering response bodies of tens of MB can stall the UI | Stream + virtualize + lazy-render collapsed regions; show only a summary above the threshold |
| OAuth 2.0 flow | Authorization Code requires launching a browser and receiving a callback | Open the system browser through `platform.open` and receive the callback on a local port |
| Original header order/casing | Go's `net/http` Header is a map and does not preserve order | Use a custom ordered representation where necessary |
| Cross-platform consistency | Windows/macOS/Linux WebViews differ | Build and smoke-test all three platforms in CI |

---

## 4. Phase 1 Task Breakdown (Ready to Implement)

Goal: complete the smallest end-to-end workflow: create request -> send -> inspect response -> save to collection -> inspect history.

**Backend (Go/Wails)**

1. Initialize Wails v2 and add goroutine + `context`, standard-library `net/http` with gzip/TLS, pure-Go `modernc.org/sqlite`, `encoding/json`, and goja for Phase 2.
2. `storage`: create the database and `PRAGMA user_version` migrator with `modernc.org/sqlite`; implement `workspace`, `node`, and `history` tables.
3. `model`: use Wails `wails generate module` to generate TypeScript bindings/types under `frontend/wailsjs/`.
4. `httpengine`: implement a minimal `SendRequest` for method/URL/headers/body/basic timing, with `context.Context` cancellation.
5. Add Wails bindings for `SendRequest`, `ListNodes`, `UpsertNode`, and `ListHistory`.
6. Connect the `request:progress` Wails event channel through `runtime.EventsEmit`.

**Frontend (`frontend/src`)**

7. Scaffold Vite + React + TypeScript + Tailwind + Radix and the typed `ipc/` wrapper.
8. Build the three-pane layout: collection tree on the left, request editor in the center, and response viewer on the right or bottom.
9. Build the request editor: method selector, URL input, Header/Query tables, and raw JSON Body editing with CodeMirror 6.
10. Build the response viewer: status/duration/size, Pretty JSON, and Headers tab.
11. Add collection-tree CRUD with create/delete/rename/drag ordering and multi-tab editing.
12. Add a virtualized history list with click-to-replay.

**Acceptance criteria**: send GET/POST requests to any public API and display responses and timing correctly; save and reopen requests in a collection; replay history. Windows 10/11 x64 and macOS 12+ on Apple Silicon and Intel builds must launch and complete one request against a local mock server. Data must be stored under the application-data directory resolved through Wails runtime paths and Go `path/filepath`. **Keep every milestone buildable and runnable, with tests for critical paths.**
