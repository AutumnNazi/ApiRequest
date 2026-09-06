# Collaboration and Synchronization (WebDAV, Implemented)

English | [简体中文](../sync.md)

> **Status: implemented on 2026-07-26.** The WebDAV design lets users bring any WebDAV service, such as Nutstore, Nextcloud, or a self-hosted instance, so the project does not operate a server. This follows the same model as open-source tools such as Joplin and Floccus. The original oplog-push placeholder remains a possible path toward real-time collaboration.

Related: [Documentation Index](./index.md) · [Data Model](./data-model.md) · [Security and Operations](./ops.md)

## Design

- **Local-first**: all data is written to local SQLite first. Synchronization is an optional layer triggered manually from the top-bar sync control, or on a configured interval (see below).
- **Remote representation**: one snapshot per workspace at `ApiRequest/workspace-<id>.json`. This is a complete state snapshot rather than an oplog; snapshots are more robust on "dumb storage" such as WebDAV and require no server-side merge logic.
- **Transport**: a minimal WebDAV client using GET/PUT/MKCOL + Basic auth. See `backend/sync/dav.go`.

## Merge Algorithm (Field-Level Three-Way Merge, ADR-017)

One synchronization cycle = pull remote -> merge -> write local -> push merged result:

1. **Baseline**: a "last-merge baseline" is persisted locally (the `sync_base` table, one per workspace, with secrets unstripped) and serves as the common ancestor for the three-way merge; it is updated to the merged result after every successful local write. The baseline is kept even if the push fails: the remote still holds the old snapshot, so the next round re-incorporates the remote delta.
2. **Node content fields** (name/request/auth/variables/preScript/testScript/sortOrder): three-way, field by field; within a request the merge recurses per key (arrays such as headers/params are atomic keys). If local matches the baseline take remote; if remote matches the baseline take local; if both sides changed the same field, **local wins** and the conflict is listed in the sync result panel (entity, field, and both value previews). When the merged result differs from both sides, both counters increase.
3. **Structural operations stay entity-level LWW**: soft-delete tombstones (`deleted_at`; a newer deletion beats an edit and entities are not resurrected) and moves (parent changes follow the newer `rev = max(updatedAt, deletedAt)`). Without a baseline (first sync, first round after an upgrade, or a corrupted baseline) the round falls back to entity-level LWW and rebuilds the baseline.
4. **Environments**: three-way on the `name` and `variables` fields; `is_active` is not synchronized (local UI state).
5. **Global variables**: remain whole-set LWW (an independent `updated_at` as the overall revision), not per-field.
6. **Compatibility**: the baseline is validated against the snapshot `schemaVersion` and rebuilt automatically when the version differs; older peers without baseline semantics are unaffected (they merge independently with their own baseline or fallback).

## Sensitive Data

- Users can enable **Omit secret variable values** (`OmitSecrets`). Before upload, remove request/collection auth credentials and each `type=secret` variable value while retaining placeholders, and write `secretsOmitted: true` in v2 snapshots. Only empty placeholders carrying that snapshot marker are restored from local values by entity ID, variable key, and duplicate-key occurrence; an unmarked empty value is an explicit user clear. Each device maintains its own secrets.
- WebDAV passwords and request credentials use the system keychain when available. If it is unavailable, an Argon2id + AES-GCM encrypted file is used; SQLite stores only `secret://...` references.

## Versions and Failure Handling

- The current snapshot `schemaVersion` is 2. If the remote version is newer, refuse to merge and prompt the user to upgrade the client. Version 1 had no `secretsOmitted` metadata, so empty secret fields are conservatively treated as stripped placeholders to preserve compatibility with older omit-secret snapshots.
- WebDAV snapshot GET and PUT operations enforce a 64 MiB limit and reject oversized data instead of silently truncating it before merge.
- Report remote 401/403 responses as explicit authentication failures. Treat 404 as first-time initialization and upload directly.
- Report counts of pushed, pulled, and deleted entities together with the remote path; display the result in the frontend top bar.

## Known Limitations and Future Work

- ~~No automatic scheduled sync~~ Automatic sync is now supported (interval in minutes configured in Settings; 0 = off): the app checks for due workspaces every 30 seconds and triggers them independently per workspace. Failed attempts also count toward the interval (preventing a 30s retry storm against an unavailable server); on completion the top bar shows a light notice and local data refreshes. Triggering the same workspace concurrently on this device fails fast with an "already in progress" error (no queuing). Cross-device mutual exclusion uses WebDAV conditional writes: each PUT carries the ETag observed at pull time (If-Match); if the remote changed concurrently (412), the engine re-pulls, re-merges, and re-pushes (up to 3 attempts). Servers without ETag support degrade to unconditional writes (last-writer-wins).
- Cookies and history do not synchronize because they are local runtime data.
- The remote snapshot path is derived from the local `workspaceId`. The desktop app currently synchronizes existing local workspaces only; it does not discover or import remote workspaces. Reusing one snapshot across devices therefore requires preserving the same workspace ID, for example by migrating the local data directory.
