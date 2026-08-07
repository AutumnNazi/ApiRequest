# ApiRequest Domain Context

English | [简体中文](./CONTEXT.zh-CN.md)

## Core Modules

- **Workspace Session**: the workspace-scoped editor state containing tabs, the active tab, and recoverable drafts. A tab always belongs to exactly one workspace.
- **Secret Vault**: the only Module allowed to persist structured credential fields, including authentication parameters, secret variables, sync passwords, and Cookie values. It stores opaque `secret://` references in SQLite and resolves them at runtime through a system keychain Adapter or an encrypted-file Adapter.
- **Redactor**: the Secret Vault policy that irreversibly replaces credential values before data crosses audit, history, log, export, or secret-omitting sync boundaries.
- **History Summary**: the lightweight, pageable projection used by the history list. It never includes request snapshots, response headers, test results, or response bodies.
- **History Detail**: the on-demand replay record for one history item. Request credentials, response `Set-Cookie` values, and reflected known secrets are already redacted before persistence.
- **Response Blob**: a response body stored outside SQLite. Consumers inspect metadata, read bounded ranges, or stream it to a user-selected file; they do not assume the whole blob fits in memory.
- **Request Progress**: the lifecycle state emitted for a send: `sending`, `ttfb`, `downloading`, and `done`, with received byte counts where available.
- **Operation Lifecycle**: the registry-backed identity, parent cancellation, completion, and shutdown semantics shared by Request Execution and Collection Runner operations. An active operation ID is unique until completion.

## Invariants

1. Persisted structured secret fields (Auth secret parameters, `type=secret` variables, the sync password, and non-empty Cookie values) are opaque `secret://keyring/` or `secret://file/` references. Arbitrary strings in URLs, ordinary headers, bodies, and scripts remain request content and are not automatically classified as credentials.
2. History and logs are audit surfaces, not secret recovery surfaces. Redaction there is irreversible.
3. Node identity includes workspace ownership. Updates, moves, ancestor lookup, request sends, and environment selection must reject cross-workspace references.
4. List Interfaces return bounded summaries. Large detail and body payloads require explicit detail or range Interfaces.
5. Closing a dirty draft requires an explicit user decision. Recoverable drafts are stored per Workspace Session, with structured auth credentials omitted from the frontend persistence copy.
6. Canceling a parent operation propagates to its active child request. Application shutdown stops accepting new operations, cancels active work, and waits for completion before closing storage.

## Adapters

- **System Keychain Adapter**: preferred Secret Vault implementation, backed by the operating system credential store.
- **Encrypted File Adapter**: fallback implementation, encrypted with AES-GCM using an Argon2id-derived key. Its master password exists only in memory and the Adapter starts locked after process restart.
- **Native Dialog Adapter**: Wails file and directory dialogs used for paths that must be selected or written by the desktop host.
