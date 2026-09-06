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
- **Boundary**: only `backend/platform` may call the OS keychain directly. `backend/secrets` owns Vault policy, fallback persistence, reference formats, and error normalization; other business modules do not know which backend is active. Secret changes and SQLite writes use a recoverable Vault batch: a database failure restores old values or deletes new entries without pretending the two systems provide a shared ACID transaction. See [ops.md](./ops.md#1-security-considerations).

### ADR-015 Persist Runner Run Reports to the Database (Accepted 2026-09-04; Reverses the Earlier In-Memory Decision)

- **Decision**: a Runner run report is written to the SQLite `runner_run` table when the run finishes (summary columns plus a detail JSON column, schema 0010). The Runner dialog offers a "run history" view for reviewing past runs, deleting one, or clearing all. This reverses the earlier "explicitly not persisted" entry in data-model.md; this ADR records that reversal.
- **Rationale**: with memory-only storage, reports vanished on restart, and checking "how did the last run go" required manual exports. Run history is a routine regression-comparison need, and the summary list query (without the detail JSON) is cheap.
- **Tradeoffs**: database size grows with run count (details include every assertion result). Reports contain request names and status codes but no request/response body payloads, so the exposure surface matches the history table. A persistence failure logs a warning and never blocks returning the report, keeping the core path independent of storage health.
- **Alternatives**: keep memory-only plus manual export (rejected: poor experience); write reports to a directory of JSON files (loses workspace isolation and easy cleanup).
- **Compatibility**: `runner.Report` gains `createdAt` (0 for in-memory reports, absent from exported JSON). Cursor pagination matches History (an opaque (created_at, id) cursor). The CLI `--report` file path is unaffected.

### ADR-016 Isolate the Cookie Jar per Workspace (Accepted 2026-09-04; Lands the Evolution Path Noted in data-model.md)

- **Decision**: the cookie table gains a `workspace_id` column (schema 0011 rebuilds the table, `UNIQUE(workspace_id, domain, path, name)`); the jar and request sending are isolated per workspace. Existing cookies are assigned to the earliest-created workspace.
- **Rationale**: under the previous cross-workspace sharing, different projects hitting the same domains (SSO, api.example.com) polluted each other's login state — cross-environment leakage is a frequent API-testing incident. Each workspace now owns its sessions.
- **Tradeoffs**: previously shared login state is now visible only in the first workspace (re-login restores it, an acceptable cost); CookieManager and the binding methods all carry a workspaceId (a breaking interface change; wailsjs regenerated).
- **Alternatives**: keep a global jar plus a `workspace_id IN (ws, '')` fallback (rejected: the same (domain, path, name) can match both a global row and a workspace row, emitting duplicate cookies that require extra de-duplication); environment-level isolation (too fine-grained: staging and prod often share a domain and differ by path, so switching by environment harms reuse).
- **Boundary**: deleting a workspace cascades to clear its jar (`ON DELETE CASCADE`); Vault reference format is unchanged (`cookie/<id>/value`) and the migration preserves row ids, so secret references need no rewrite.

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

### ADR-017 Field-Level Sync Merge: Three-Way Merge + Local "Last-Merge Baseline" (Preferred, Not Yet Implemented)

- **Context**: the current behavior is entity-level LWW (item 5 of the merge algorithm in [sync.md](./sync.md)) — when two devices change different fields of the same request, the later writer replaces the whole entity and the earlier field change is silently lost.
- **Design**: persist a local "last-merge baseline" (one per workspace, the complete snapshot of the most recent merge result) and perform a three-way, field-by-field merge per entity: local matches baseline → take remote (local untouched); remote matches baseline → take local (remote untouched); both sides changed the same field → conflict. Snapshots are already complete state (not an oplog), so the baseline naturally serves as the common ancestor without changing the remote protocol.
- **Conflict policy**: deterministic "local wins + conflict list" — no interactive merge UI; the sync result panel lists conflict details (entity, field, values on both sides), the user reviews and fixes manually, and a one-click "overwrite with remote" action can be added later. This matches the lightweight positioning of "dumb storage + manual trigger".
- **Structural operations are not merged per field**: moves (parent changes) and soft-delete tombstones still follow entity-level LWW — a tombstone newer than any field edit wins and the entity stays dead; when a move races an edit, the move wins (the edited fields travel with the entity and are not lost). Child ordering (`order`) is treated as a regular field: concurrent reordering on both sides is a conflict, local wins.
- **Baseline storage**: new SQLite table `sync_base(workspace_id, schema_version, snapshot, merged_at)`; snapshots are already capped at 64 MiB, so the baseline is bounded. When no baseline exists (first sync, or the first round after an upgrade), that round falls back to entity-level LWW, and the baseline is written after the merge completes.
- **Tradeoffs**: local storage for synced workspaces roughly doubles (snapshot-level baseline); merge cost rises from one comparison per entity to per-field comparisons. In exchange, concurrent edits no longer lose fields.
- **Alternatives**: per-field timestamps / field revs (rejected: every write path would need instrumentation, too invasive); CRDTs (rejected: the snapshot model has no oplog to replay, equivalent to rewriting the sync layer); an interactive conflict-resolution UI (deferred: land the deterministic policy first, add the UI only if real demand appears).
- **Reversal condition**: if conflicts turn out to be rare in practice (users almost never edit the same entity concurrently), the complexity is not worth it and entity-level LWW stays.

### ADR-018 Signed Update Protocol: Manifest + ed25519 + Channel Files + Two-Stage Atomic Replacement (Preferred, Not Yet Implemented)

- **Context**: OPEN-006 currently only "checks and redirects to the release page"; in-app updates must first settle signature verification, channels, atomic replacement, and rollback. This ADR completes that checklist, narrowing OPEN-006's open item to "implementation scheduling".
- **Protocol**: each release ships `update-manifest.json` channel files (`stable.json` / `beta.json`) containing the version, release time, a minimum upgradable version floor, and per-platform entries {url, sha256, size, signature}. The manifest body is signed with the release ed25519 private key; the signature is published alongside `SHA256SUMS`; the matching public key is pinned in the client. ed25519 uses Go's standard `crypto/ed25519` (pure Go, no CGO, per the ADR-009 constraint).
- **Verification chain**: download the package → verify sha256 → verify the ed25519 signature against `SHA256SUMS` → check the version floor (below the floor, direct the user to the download page for a manual install; no silent path) → only then enter the replacement stage. Any failure discards the download and keeps the current state; no retry loops.
- **Replacement and rollback**: two-stage atomic replacement — write the new package to a temporary file next to the data directory (same-volume rename is atomic); stage one backs up the current binary, stage two renames the new file into place; a rename failure restores the backup. On Windows a running exe is locked, so use a `pending-update` marker with replacement at exit or on next launch, and verify on first start after reboot — a version mismatch triggers rollback. A leftover `pending-update` marker at startup is completed or rolled back, then cleared.
- **Channels**: stable / beta manifest files; beta can be reached from stable but stable never auto-downgrades to beta. The channel is selected in Settings and defaults to stable.
- **Key management**: the release private key exists only on the release machine (injected into GitHub Actions via a repository secret); the public key ships with the client. Rotation = a new public key is embedded in the next client version, with a one-version dual-signature transition (either signature passing is accepted); older clients are unaffected.
- **Alternatives**: SHA256SUMS without signatures (rejected: integrity without authenticity — a mirror or proxy could swap packages); sigstore/cosign (rejected: pulls in CLI dependencies and KMS assumptions, beyond a desktop tool's positioning); keep the "redirect to download page" flow (retained as the fallback for versions below the floor and for manual updates).
- **Reversal condition**: if most users in practice update via package managers (winget/brew), the in-app updater sees little use and the redirect-only policy can stay.

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

- **Decision**: use JSON isomorphic to the internal IR: `collection.json`, one `*.request.json` file per request, nested directories with `_folder.json`, and forward-compatible `schemaVersion`. Slugs use the common safe subset across Windows, macOS, and Linux. Case-insensitive sibling collisions and metadata filename conflicts receive a stable node suffix, and mirror JSON is never read or written through symlinks. See `backend/mirror`.

### OPEN-005 License

- The README marks this as undecided. Product-level confirmation is required for open source (MIT/Apache-2.0) versus closed source.

### OPEN-006 Automatic Updates (Interim Decision: Check and Redirect Only)

- **Background**: Wails does not provide a built-in signed update pipeline.
- **Current decision (2026-08-04)**: the release workflow publishes only packages and `SHA256SUMS`; Settings only checks and opens the official GitHub release download page. No update manifest is published and no binary is replaced silently until a signature-verification protocol is in place.
- **Interim decision (2026-09-06)**: the signature verification chain is implemented (`backend/updater`: channel manifest + ed25519 verification + version floor; the `cmd/updatetool` signing tool). It is disabled by default — "Check for updates" in Settings only works once `update.manifestUrl` is configured, and results are display-only: nothing is downloaded or replaced. Silent replacement and rollback remain deferred per ADR-018's implementation schedule.

---

## Decision Process

- Append new or changed decisions to this file and update every affected design document.
- When an open item is decided, move it into Accepted Decisions and record the date and rationale.
- Breaking contract or schema changes must reference the versioning process in `data-model.md`.
