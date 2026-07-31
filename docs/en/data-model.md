# Data Model, Storage, and Shared Types

English | [简体中文](../data-model.md)

This document covers the domain model, SQLite schema, shared frontend/backend type contract, schema migrations, and version compatibility.

Related: [Documentation Index](./index.md) · [Overview](./overview.md) · [Request Lifecycle](./request-lifecycle.md)

---

## 1. Data Model (Domain Entities)

```text
Workspace
 `-- Collection
      |-- Folder (nestable)
      |    `-- Request
      |-- auth (collection-level, inheritable)
      |-- variables (collection-level)
      `-- scripts (collection-level pre-request/test scripts, inheritable)

Environment
 `-- variables[] (key/value + secret flag + enabled state)

Request
 |-- method / url
 |-- query params[]
 |-- headers[]
 |-- body (none | form-data | urlencoded | raw(json/xml/text) | binary | graphql)
 |-- auth
 |-- preRequestScript / testScript
 `-- settings (overrides for timeout/redirects/SSL verification/etc.)

History: one actually sent request snapshot + its response
Response: status / headers / body / cookies / timing / size
Example: an example response attached to a Request (status/headers/body),
  created by "Save as Example" or imported from OpenAPI for documentation and Mock Server matching
```

**Variable-scope precedence** (high -> low): local/temporary variables -> Runner data file -> environment variables -> collection variables -> global variables. The resolver searches for `{{var}}` in this order. See [Variable Resolution and Template Engine](./request-lifecycle.md#2-variable-resolution-and-template-engine).

---

## 2. Database Schema Design

Use a single-file SQLite database. Every primary key is `TEXT` containing UUID v7, whose natural time ordering helps indexing and pagination. Timestamps are `INTEGER` Unix milliseconds. Soft deletion uses `deleted_at` to support synchronization and a recycle bin.

```sql
-- Workspaces
CREATE TABLE workspace (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL DEFAULT 'local',   -- local | team
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

-- Collections, folders, and requests share a tree node with self-referencing parent_id.
CREATE TABLE node (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL REFERENCES workspace(id),
  parent_id     TEXT REFERENCES node(id),        -- NULL = collection root
  kind          TEXT NOT NULL,                    -- collection | folder | request
  name          TEXT NOT NULL,
  sort_order    REAL NOT NULL DEFAULT 0,          -- Floating point supports insertion between items.
  -- Request-only fields (kind=request) are stored as JSON in request_data.
  request_data  TEXT,                             -- JSON: method/url/params/headers/body/auth/scripts/settings
  -- Inheritable collection/folder configuration.
  auth          TEXT,                             -- JSON; descendants may inherit it.
  variables     TEXT,                             -- JSON key/value entries.
  pre_script    TEXT,
  test_script   TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  deleted_at    INTEGER
);
CREATE INDEX idx_node_ws_parent ON node(workspace_id, parent_id);

-- Environments
CREATE TABLE environment (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL REFERENCES workspace(id),
  name          TEXT NOT NULL,
  variables     TEXT NOT NULL DEFAULT '[]',       -- JSON: [{key,value,type:default|secret,enabled}]
  is_active     INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

-- One global-variable row per workspace.
CREATE TABLE global_var (
  workspace_id  TEXT PRIMARY KEY REFERENCES workspace(id),
  variables     TEXT NOT NULL DEFAULT '[]'
);

-- History stores the sent snapshot and response summary; large bodies live in files below.
CREATE TABLE history (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL,
  request_snap  TEXT NOT NULL,                    -- JSON: snapshot of the actual request sent.
  status        INTEGER,
  duration_ms   INTEGER,
  size_bytes    INTEGER,
  response_meta TEXT,                             -- JSON: headers/cookies/timing
  body_ref      TEXT,                             -- Relative path to a large body in the blobs directory.
  test_results  TEXT,                             -- JSON: [{name,pass,error}]
  created_at    INTEGER NOT NULL
);
CREATE INDEX idx_history_ws_time ON history(workspace_id, created_at DESC);

-- Examples are saved responses attached to requests and are also Mock Server data sources.
CREATE TABLE example (
  id            TEXT PRIMARY KEY,
  node_id       TEXT NOT NULL REFERENCES node(id),  -- Owning request (kind=request).
  name          TEXT NOT NULL,
  request_snap  TEXT,                               -- Optional JSON snapshot that triggers this example.
  status        INTEGER NOT NULL,
  headers       TEXT NOT NULL DEFAULT '[]',         -- JSON: KV[]
  body          TEXT,                               -- Text body; saving very large examples is discouraged.
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  deleted_at    INTEGER
);
CREATE INDEX idx_example_node ON example(node_id);

-- Cookie Jar is shared across workspaces, like browser behavior: cookies are isolated by domain,
-- not by business grouping. Add workspace_id and migrate if workspace isolation is needed later.
CREATE TABLE cookie (
  id          TEXT PRIMARY KEY,
  domain      TEXT NOT NULL,
  path        TEXT NOT NULL DEFAULT '/',
  name        TEXT NOT NULL,
  value       TEXT NOT NULL,
  expires_at  INTEGER,
  http_only   INTEGER NOT NULL DEFAULT 0,
  secure      INTEGER NOT NULL DEFAULT 0,
  same_site   TEXT,
  UNIQUE(domain, path, name)
);

-- Application settings (key/value).
CREATE TABLE setting (key TEXT PRIMARY KEY, value TEXT NOT NULL);

-- Optional operation log for synchronization.
CREATE TABLE oplog (
  id           TEXT PRIMARY KEY,
  entity       TEXT NOT NULL,                     -- node | environment | ...
  entity_id    TEXT NOT NULL,
  op           TEXT NOT NULL,                     -- upsert | delete
  payload      TEXT,
  lamport      INTEGER NOT NULL,                  -- Logical clock for conflict ordering.
  synced       INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL
);
```

**Design notes**:

- Collections, folders, and requests share a self-referential `node` tree, simplifying move, copy, and ordering logic. `request_data` uses JSON instead of expanded columns to avoid frequent schema changes.
- Large response bodies do not enter the database. Write them under `blobs/` in the application-data directory resolved by `platform.paths`; store only a reference and summary in the database to prevent SQLite growth.
- Persist `body_ref` and paths in collection mirrors as logical paths relative to their respective roots, always separated by `/`. Resolve them through path APIs at runtime; never concatenate Windows/macOS path strings.
- `sort_order` is floating point. Insert between two items by taking the midpoint instead of reordering the entire collection.
- Store the schema version in `PRAGMA user_version` and run migrations in version-number order, as described below.
- The storage layer also offers an optional directory-mirror mode that serializes collections into directory trees and JSON files for Git-based team workflows, similar to Insomnia and Bruno.

**Data explicitly not persisted**:

- **Runner reports**: keep them in memory only. At the end of a run, users export JSON/HTML; see [advanced.md](./advanced.md#2-collection-runner). Do not persist them.
- **gRPC proto descriptors**: keep descriptors obtained through server reflection in memory only. Store manually imported `.proto` files or FileDescriptorSets under the `protos/` directory resolved by `platform.paths`; the database `setting` table stores only path references.
- **GraphQL introspection schema**: memory + disk cache under `cache/`. It can be fetched again at any time and does not belong in the database.

---

## 3. Shared Frontend/Backend Types (Contract)

Frontend types are generated from Go structs as the single source of truth through Wails' built-in `wails generate module` Go-to-TypeScript bindings. This prevents handwritten drift. Core contracts:

```ts
// Request
interface HttpRequest {
  method: string;
  url: string;
  params: KV[];              // Query parameters.
  headers: KV[];
  body: Body;
  auth: Auth;
  settings: RequestSettings;
  preScript?: string;        // Request-level pre-script, merged with collection/folder scripts.
  testScript?: string;       // Request-level test script.
}
// The Body type below is shown as a TS discriminated union for convenient narrowing.
// Wails actually generates a flat Body struct where Kind and all other fields exist.
// The frontend narrows it with a guard such as `if (body.kind === 'formdata')`.
// This union documents which field set is active for each kind.
type Body =
  | { kind: 'none' }
  | { kind: 'raw'; language: 'json'|'xml'|'html'|'text'; text: string }
  | { kind: 'formdata'; items: FormItem[] }
  | { kind: 'urlencoded'; items: KV[] }
  | { kind: 'binary'; path: string }
  | { kind: 'graphql'; query: string; variables: string };

interface KV { key: string; value: string; enabled: boolean; description?: string }

// Phase 1 passes Auth through; backend providers registered through auth.Register implement it.
// Valid Type values: "" | "none" | "inherit" (no lookup) plus every registered provider:
// basic | bearer | apikey | oauth1 | oauth2 | digest | awsv4
interface Auth {
  type: string;
  params?: Record<string, string>;
}

interface FormItem {
  key: string;
  type: 'text' | 'file';   // path applies to file; value applies to text.
  value?: string;
  path?: string;
  enabled: boolean;
}

// Send context assembled by the frontend and passed to SendRequest.
interface SendContext {
  requestId?: string;
  environmentId?: string;
  // Temporary/local variable overrides known to the frontend.
  variableOverrides?: Record<string, string>;
}

// Response result
interface ResponseResult {
  status: number;
  statusText: string;
  headers: KV[];
  cookies: Cookie[];
  body: ResponseBody;         // Inline small bodies; return a ref for large bodies.
  timing: Timing;
  sizeBytes: number;
  testResults: TestResult[];
  scriptLogs: string[];       // Captured console.log output.
}
interface Timing {
  dnsMs: number; connectMs: number; tlsMs: number;
  ttfbMs: number; downloadMs: number; totalMs: number;
}
```

`KV`, `Body`, `Auth`, and related types are shared by frontend and backend and form the application's stable contract surface. Version every change as described below.

---

## 4. Schema Migrations and Version Compatibility

- **Database migrations**: record the version in `PRAGMA user_version`. At startup, apply `migrations/NNNN_*.sql` in order and back up the database file before every migration.
- **Export format versions**: include `schemaVersion` in the internal model. Upgrade older imports through adapters. Export the latest version by default and optionally target an older supported version.
- **Contract changes**: any breaking change to the shared types in section 3 must bump the version and include frontend compatibility handling.
