# Collaboration and Synchronization (WebDAV, Implemented)

English | [简体中文](../sync.md)

> **Status: implemented on 2026-07-26.** The WebDAV design lets users bring any WebDAV service, such as Nutstore, Nextcloud, or a self-hosted instance, so the project does not operate a server. This follows the same model as open-source tools such as Joplin and Floccus. The original oplog-push placeholder remains a possible path toward real-time collaboration.

Related: [Documentation Index](./index.md) · [Data Model](./data-model.md) · [Security and Operations](./ops.md)

## Design

- **Local-first**: all data is written to local SQLite first. Synchronization is an optional layer triggered manually from the top-bar sync control.
- **Remote representation**: one snapshot per workspace at `ApiRequest/workspace-<id>.json`. This is a complete state snapshot rather than an oplog; snapshots are more robust on "dumb storage" such as WebDAV and require no server-side merge logic.
- **Transport**: a minimal WebDAV client using GET/PUT/MKCOL + Basic auth. See `backend/sync/dav.go`.

## Merge Algorithm (Entity-Level LWW)

One synchronization cycle = pull remote -> merge -> write local -> push merged result:

1. **Nodes** (collections/folders/requests): for the same ID, compare `rev = max(updatedAt, deletedAt)` and keep the larger revision; preserve entities that exist on only one side.
2. **Deletion propagation**: a soft deletion is a tombstone in LWW. If the deletion is newer than an update, deletion wins and the entity is not resurrected.
3. **Environments**: apply LWW by ID using `updatedAt`; do not synchronize `is_active`, which is local UI state.
4. **Global variables**: use one revision for the complete set, approximated by the latest workspace modification time, and apply LWW to the entire set.
5. **Conflict granularity**: entity-level. If two devices change different fields on the same request, the later writer replaces the entire entity. Field-level merge or CRDTs may be evaluated later.

## Sensitive Data

- Users can enable **Omit secret variable values** (`OmitSecrets`). Before upload, remove the `value` of each `type=secret` variable while retaining its key placeholder. After pulling and merging, restore any locally available value. Each device maintains its own secrets.
- The WebDAV password is currently stored in the local `setting` table. Moving it to the system keychain through the ADR-013 secrets backend is future work.

## Versions and Failure Handling

- Snapshots carry `schemaVersion`. If the remote version is newer, refuse to merge and prompt the user to upgrade the client.
- Report remote 401/403 responses as explicit authentication failures. Treat 404 as first-time initialization and upload directly.
- Report counts of pushed, pulled, and deleted entities together with the remote path; display the result in the frontend top bar.

## Known Limitations and Future Work

- There is no automatic scheduled sync and no pre-sync mutual-exclusion lock. Simultaneous PUTs use last-writer-wins; in an extreme race, one peer's snapshot may be lost for one cycle and recovered on the next sync.
- Cookies and history do not synchronize because they are local runtime data.
- Binding multiple devices to the same remote workspace requires the same workspace ID. Pulling a remote workspace for the first time automatically creates a local workspace with that ID.
