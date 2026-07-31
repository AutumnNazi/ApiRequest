# Architecture Decision Records (ADRs)

English | [简体中文](../decisions.md)

This document records the **decisions, rationale, tradeoffs, and alternatives** behind key technical choices, together with **open questions** that still need a decision. Append changes rather than deleting old entries so the design's evolution remains visible.

Status markers: `Accepted` = adopted and reflected in the design docs; `Preferred` = recommended but reversible; `Open` = decision required.

---

## Accepted Decisions

### ADR-000 Use Go for the Backend and Wails v2 for the Desktop Framework (Accepted)

- **Decision**: use **Go** for the backend and **Wails v2** for the desktop framework (Go backend + system WebView + frontend).
- **Rationale**: Go's standard library covers the project's core needs well. `net/http` + `net/http/httptrace` provide full network control and phase timing; dynamic gRPC invocation is a first-class Go capability; Wails generates Go-to-TypeScript bindings from one source of truth; and goroutine + `context` provide a straightforward concurrency and cancellation model.
- **Hard constraint**: use **pure-Go dependencies** such as goja and `modernc.org/sqlite`, avoiding CGO so all three platforms cross-compile cleanly. Do not introduce CGO dependencies such as v8go or `mattn/go-sqlite3`.

### ADR-001 Use Wails Instead of Electron or a Pure Web Application (Accepted)

- **Rationale**: request execution must bypass WebView CORS and same-origin restrictions while fully controlling TLS, proxies, original headers, and phase timing. These capabilities require a native layer. Wails uses the system WebView and has substantially lower package size and memory use than Electron.
- **Tradeoffs**: give up Electron's mature ecosystem and built-in Node capabilities; accept compatibility work across platform WebViews, covered by three-platform CI smoke tests; and build a custom update path because Wails has no built-in updater (see OPEN-006).
- **Alternatives**: Electron, rejected for size; pure Web, rejected because the browser sandbox prevents core networking capabilities.

### ADR-002 Execute Requests in Go (Accepted)

- **Rationale**: see `overview.md` section 2.1. Native network control, accurate `httptrace` timing, and streaming large responses.
- **Tradeoff**: the frontend cannot send requests directly; all network operations use Wails bindings. In return, the system gains capability and testability.

### ADR-003 Use React + TypeScript for the Frontend (Accepted, User Confirmed)

- **Rationale**: broadest ecosystem, strong component availability, and high team familiarity.
- **Alternatives**: Svelte/Solid are lighter but have smaller ecosystems; Vue was viable, but the user selected React.

### ADR-004 Model Collections, Folders, and Requests as a Self-Referential `node` Tree (Accepted)

- **Rationale**: unify move, copy, ordering, and inheritance logic instead of maintaining three structurally similar tables. See `data-model.md`.
- **Tradeoff**: queries must filter on `kind`, and request-specific fields live in a JSON column.

### ADR-005 Store Request Details in a JSON Column (Accepted)

- **Rationale**: request structure evolves frequently as body types, auth types, and settings are added. A JSON column avoids frequent table alterations and migrations.
- **Tradeoff**: request-internal fields cannot be queried or indexed directly in SQL, such as "all requests using this header." Such queries are expected to be rare and can run in the application layer, so the tradeoff is favorable.
- **Alternatives**: fully expanded columns require frequent migrations; EAV tables produce complex and slow queries.

### ADR-006 Use UUID v7 Primary Keys (Accepted)

- **Rationale**: time ordering improves index locality and creation-time pagination; distributed generation avoids conflicts and supports future synchronization.
- **Tradeoffs**: 16 bytes rather than 8 for an incrementing integer and slightly more complexity than UUID v4. The difference is negligible at local-database scale.
- **Alternatives**: incrementing integers conflict during synchronization; UUID v4 is unordered and fragments indexes. Generate v7 in Go through `google/uuid` or `gofrs/uuid`.

### ADR-007 Generate Shared TypeScript Types with Wails (Accepted)

- **Rationale**: during builds, Wails generates TypeScript types and call wrappers from bound Go structs and method signatures. This provides one frontend/backend source of truth with no handwritten drift or separate generation pipeline.
- **Tradeoff**: Wails controls the shape of generated artifacts. Complex types such as the discriminated `Body` union may require matching Go structs/tags and frontend narrowing. Keep a small set of handwritten supplemental types where direct mapping is impossible.
- **Alternative**: handwritten TypeScript types, rejected because they drift.
- **Note**: `data-model.md` fixes the contract itself. The generation mechanism is an implementation detail to finalize during Phase 1.

### ADR-008 Use goja as the Script Engine (Pure-Go JavaScript) (Accepted)

- **Rationale**: goja is pure Go, avoids CGO, embeds and sandboxes cleanly, supports ES5.1 plus much of ES6, and can interrupt timeouts through `context` + `vm.Interrupt`. It fits the isolated "new runtime per request" model.
- **Tradeoff**: performance and language coverage are behind V8, while v8go's CGO dependency breaks easy cross-compilation. Request scripts are lightweight assertions and variable transformations, so goja is sufficient.
- **Alternatives**: v8go is faster but requires CGO and is rejected; otto is older and less actively maintained.
- **Sandbox constraints**: do not expose `require`, the filesystem, or arbitrary network access. Networking is available only through the controlled `pm.sendRequest` bridge to the Go HTTP engine. Create a fresh goja Runtime for each request to prevent global-state leakage.

### ADR-009 Use SQLite through `modernc.org/sqlite` for Local Storage (Accepted)

- **Rationale**: SQLite is mature, transactional, easy to back up and migrate, and stores the database in one file. Large response bodies live in `blobs/`. The pure-Go `modernc.org/sqlite` implementation avoids CGO and supports cross-compilation.
- **Tradeoff**: `modernc.org/sqlite` is somewhat slower than the CGO-based `mattn/go-sqlite3`, but it is more than adequate for local single-user load, and portability matters more.
- **Alternatives**: `mattn/go-sqlite3` performs well but requires CGO and is rejected; custom file formats reinvent storage; embedded KV stores have weak query support.

### ADR-013 Use the System Keychain by Default, with Master-Password Encryption as Fallback (Accepted)

- **Decision**: use `zalando/go-keyring` to access Windows Credential Manager and macOS Keychain for secret variables, OAuth access tokens, and refresh tokens. Never write secret values into SQLite, history, or collection mirrors. If the system keychain is unavailable, require a master password and derive a key with `golang.org/x/crypto/argon2` (Argon2id) to encrypt local secret data.
- **Rationale**: reuse the OS credential protection and unlock model by default for a consistent Windows/macOS security experience. The fallback keeps core features usable in CI, restricted enterprise environments, and Linux systems without a GUI keychain.
- **Tradeoff**: fallback mode requires entering a master password on first use. A forgotten master password cannot recover encrypted secrets; users must clear and reauthorize them.
- **Boundary**: only the `secrets` implementation in the `platform` package may call the keychain, store fallback data, and normalize errors. Business modules must not know which platform backend is active. See [ops.md](./ops.md#1-security-considerations).

---

## Preferred Decisions (Recommended, Reversible)

### ADR-010 Prefer CodeMirror 6 over Monaco

- **Rationale**: CM6 is much smaller, which helps the package-size budget in `ops.md` (< 30 MB per platform), and language packages can load on demand.
- **Tradeoff**: Monaco has stronger completion and large-file performance, but CM6 is sufficient for this tool's JSON and script editing.
- **Reversal condition**: switch to Monaco and relax the size budget if script editing needs IDE-grade completion and diagnostics.

### ADR-011 Schedule Collaboration Sync and gRPC Later

- **Rationale**: establish a solid single-user experience first. Synchronization requires oplog/Lamport conflict merging, and gRPC requires dynamic proto parsing; both are complex and outside the core loop.
- **Placement**: roadmap Phase 4 for gRPC and Phase 5 for synchronization. See `roadmap.md`.

### ADR-012 Collect Phase Timings with `net/http/httptrace` (Accepted)

- **Rationale**: Go's standard `net/http/httptrace` package exposes DNS start/end, connection establishment, TLS handshake, and first-byte callbacks as first-class features. `httptrace.ClientTrace` markers can populate every `Timing` field.
- **Reversal condition**: effectively none; this is a first-class standard-library capability with no custom implementation cost.

---

## Open Questions (Decision Required)

### OPEN-001 Include a Headless CLI Runner (Accepted: Implemented 2026-07-26)

- **Decision**: include `cmd/cli` (`apirequest-cli`). The `run` command executes collections headlessly with data files, iterations, `stopOnError`, and JSON reports. The `list` command lists workspaces and collections. It shares the desktop application's core and local database. Exit code = failed request count, capped at 100.
- **Use case**: run collections in CI with reports and exit codes, similar to Newman.

### OPEN-002 Merged into ADR-013

The former open question about secret storage was resolved by [ADR-013](#adr-013-use-the-system-keychain-by-default-with-master-password-encryption-as-fallback-accepted). The number remains for historical continuity.

### OPEN-003 Support Multiple Windows

- **Use case**: multi-monitor use and side-by-side request comparison.
- **Impact**: state management must distinguish global stores from per-window stores and synchronize where required. Wails multi-window support evolves by version, so confirm capabilities in the target release.
- **Recommendation**: ship v1 with one window and multiple tabs; defer multiple windows.

### OPEN-004 Define the Collection Mirror Directory Format (Accepted: Implemented 2026-07-26)

- **Decision**: use JSON isomorphic to the internal IR: `collection.json`, one `*.request.json` file per request, nested directories with `_folder.json`, slug-safe filenames, and forward-compatible `schemaVersion`. See `backend/mirror`.

### OPEN-005 License

- The README marks this as undecided. Product-level confirmation is required for open source (MIT/Apache-2.0) versus closed source.

### OPEN-006 Automatic Updates (Wails Has No Built-In Updater)

- **Background**: Wails does not provide a built-in signed update pipeline.
- **Open decision**: build an update service with signature verification or integrate a third-party updater, such as one based on GitHub Releases. Define signing, stable/beta channels, and rollback behavior.
- **Recommendation**: address this during Phase 5 polish; manually distribute MVP installers first.

---

## Decision Process

- Append new or changed decisions to this file and update every affected design document.
- When an open item is decided, move it into Accepted Decisions and record the date and rationale.
- Breaking contract or schema changes must reference the versioning process in `data-model.md`.
