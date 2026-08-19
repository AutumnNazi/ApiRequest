# 数据模型、存储与共享类型

[English](./en/data-model.md) | 简体中文

本篇覆盖领域模型、SQLite Schema、前后端共享类型契约，以及 Schema 迁移与版本兼容。

相关文档：[文档索引](./index.md) · [概览](./overview.md) · [请求生命周期](./request-lifecycle.md)

---

## 1. 数据模型（领域实体）

```
Workspace（工作区）
 └── Collection（集合）
      ├── Folder（文件夹，可嵌套）
      │    └── Request（请求）
      ├── auth（集合级认证，可继承）
      ├── variables（集合级变量）
      └── scripts（集合级前置/测试脚本，可继承）

Environment（环境）
 └── variables[]（键值 + 是否密钥 + 启用状态）

Request（请求）
 ├── method / url
 ├── query params[]
 ├── headers[]
 ├── body（none | form-data | urlencoded | raw(json/xml/text) | binary | graphql）
 ├── auth
 ├── preRequestScript / testScript
 └── settings（超时/重定向/SSL 校验等覆盖项）

History（历史记录）：一次实际发送的快照 + 响应
Response：status / headers / body / cookies / timing / size
Example（示例）：挂在 Request 下的示例响应（status/headers/body），
  由"保存为示例"创建或随 OpenAPI 导入，供文档展示与 Mock Server 匹配返回
```

**变量作用域优先级**（高 → 低）：本地/临时变量 → 数据文件（Runner）→ 环境变量 → 集合变量 → 全局变量。解析器按此顺序查找 `{{var}}`。详见 [变量解析与模板引擎](./request-lifecycle.md)。

---

## 2. 数据库 Schema 设计

SQLite 单文件库。所有主键用 `TEXT`（UUID v7，天然按时间有序，利于索引与分页）。时间戳统一 `INTEGER`（Unix 毫秒）。软删除用 `deleted_at`，便于同步与回收站。

```sql
-- 工作区
CREATE TABLE workspace (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL DEFAULT 'local',   -- local | team
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

-- 集合（文件夹与集合统一为树节点，用 parent_id 自引用）
CREATE TABLE node (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL REFERENCES workspace(id),
  parent_id     TEXT REFERENCES node(id),        -- NULL = 集合根
  kind          TEXT NOT NULL,                    -- collection | folder | request
  name          TEXT NOT NULL,
  sort_order    REAL NOT NULL DEFAULT 0,          -- 用浮点便于中间插入
  -- 请求专属字段（kind=request 时有效）以 JSON 存于 request_data
  request_data  TEXT,                             -- JSON: method/url/params/headers/body/auth/scripts/settings
  -- 集合/文件夹级可继承配置
  auth          TEXT,                             -- JSON, 可被子节点继承
  variables     TEXT,                             -- JSON 键值
  pre_script    TEXT,
  test_script   TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  deleted_at    INTEGER
);
CREATE INDEX idx_node_ws_parent ON node(workspace_id, parent_id);

-- 环境
CREATE TABLE environment (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL REFERENCES workspace(id),
  name          TEXT NOT NULL,
  variables     TEXT NOT NULL DEFAULT '[]',       -- JSON: [{key,value,type:default|secret,enabled}]
  is_active     INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

-- 全局变量（每工作区一行）
CREATE TABLE global_var (
  workspace_id  TEXT PRIMARY KEY REFERENCES workspace(id),
  variables     TEXT NOT NULL DEFAULT '[]',
  updated_at    INTEGER NOT NULL DEFAULT 0
);

-- 历史记录（发送快照 + 响应摘要；大 body 落文件，见下）
CREATE TABLE history (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL,
  request_snap  TEXT NOT NULL,                    -- JSON: 实际发送的请求快照
  status        INTEGER,
  duration_ms   INTEGER,
  size_bytes    INTEGER,
  response_meta TEXT,                             -- JSON: headers/cookies/timing
  body_ref      TEXT,                             -- 大 body 存到 blob 目录的相对路径
  test_results  TEXT,                             -- JSON: [{name,pass,error}]
  created_at    INTEGER NOT NULL
);
CREATE INDEX idx_history_ws_time ON history(workspace_id, created_at DESC);
CREATE INDEX idx_history_body_ref ON history(body_ref);

-- 示例（Example）：请求的示例响应，"保存为示例"的落点，也是 Mock Server 的数据源
CREATE TABLE example (
  id            TEXT PRIMARY KEY,
  node_id       TEXT NOT NULL REFERENCES node(id),  -- 所属请求（kind=request）
  name          TEXT NOT NULL,
  request_snap  TEXT,                               -- JSON: 触发该示例的请求快照（可空）
  status        INTEGER NOT NULL,
  headers       TEXT NOT NULL DEFAULT '[]',         -- JSON: KV[]
  body          TEXT,                               -- 示例响应体（文本；超大示例不建议保存）
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  deleted_at    INTEGER
);
CREATE INDEX idx_example_node ON example(node_id);

-- Cookie Jar（跨工作区全局共享——与浏览器行为一致，Cookie 按域名而非业务分组隔离；
-- 若后续需要按工作区隔离，加 workspace_id 列并迁移）
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

-- 应用设置（KV）
CREATE TABLE setting (key TEXT PRIMARY KEY, value TEXT NOT NULL);

-- 同步用操作日志（可选）
CREATE TABLE oplog (
  id           TEXT PRIMARY KEY,
  entity       TEXT NOT NULL,                     -- node | environment | ...
  entity_id    TEXT NOT NULL,
  op           TEXT NOT NULL,                     -- upsert | delete
  payload      TEXT,
  lamport      INTEGER NOT NULL,                  -- 逻辑时钟，用于冲突排序
  synced       INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL
);
```

**设计要点**：
- 集合/文件夹/请求统一为 `node` 自引用树，简化移动、复制、排序逻辑；`request_data` 用 JSON 而非展开成列，避免频繁改表。
- 大响应体不进 DB，写到 `platform.paths` 解析的应用数据目录 `blobs/` 下，DB 只存引用与摘要，避免 SQLite 膨胀。
- `body_ref` 与集合文件镜像中的路径均保存为相对于各自根目录、使用 `/` 分隔的逻辑路径；运行时通过 `Path` 解析，禁止拼接 Windows / macOS 路径字符串。
- `sort_order` 用浮点，两项之间插入取中值，无需整体重排。
- Schema 版本用 `PRAGMA user_version`，迁移脚本按版本号顺序执行（见下）。
- 存储层还提供"文件夹镜像"可选模式：把集合序列化为目录树 + JSON 文件，方便团队用 Git 管理（类似 Insomnia/Bruno 的做法）。

**明确不持久化的数据**（避免实现时猜测）：
- **Runner 运行报告**：仅存在于内存，运行结束由用户导出 JSON/HTML（见 [advanced.md](./advanced.md#2-collection-runner)）；不落库。
- **gRPC 的 proto 描述**：server reflection 拉取的描述只做内存缓存；用户手动导入的 `.proto` / FileDescriptorSet 以文件形式存 `platform.paths` 下的 `protos/` 目录，DB 的 `setting` 表只记路径引用。
- **GraphQL introspection schema**：内存 + 磁盘缓存（`cache/` 目录），可随时重新拉取，不进 DB。

---

## 3. 前后端共享类型（契约）

前端类型由 Go 侧结构体单一来源生成（Wails 内置 `wails generate module` 的 Go→TS 绑定），避免手写漂移。核心契约：

```ts
// 请求
interface HttpRequest {
  method: string;
  url: string;
  params: KV[];              // query
  headers: KV[];
  body: Body;
  auth: Auth;
  settings: RequestSettings;
  preScript?: string;        // 请求级前置脚本（与集合/文件夹级合并执行）
  testScript?: string;       // 请求级测试脚本
}
// 注：上面 Body 用 TS 判别联合表达（窄化方便）。Wails 实际生成的是扁平 Body struct
// （Kind + 其它字段都存在），前端用 `if (body.kind === 'formdata')` 做 type guard；
// 这里写 union 仅为说明各 kind 触发的字段集合。
type Body =
  | { kind: 'none' }
  | { kind: 'raw'; language: 'json'|'xml'|'html'|'text'; text: string }
  | { kind: 'formdata'; items: FormItem[] }
  | { kind: 'urlencoded'; items: KV[] }
  | { kind: 'binary'; path: string }
  | { kind: 'graphql'; query: string; variables: string };

interface KV { key: string; value: string; enabled: boolean; description?: string }

// 认证（Phase 1 仅透传；实际 provider 由后端 auth.Register 注册决定）
// 合法 Type："" | "none" | "inherit"（不查表）+ 后端注册表中的所有 provider：
//   basic | bearer | apikey | oauth1 | oauth2 | digest | awsv4
interface Auth {
  type: string;
  params?: Record<string, string>;
}

interface FormItem {
  key: string;
  type: 'text' | 'file';   // file 时 path 生效，text 时 value 生效
  value?: string;
  path?: string;
  enabled: boolean;
}

// 发送上下文（前端组装后传给 SendRequest）
interface SendContext {
  requestId?: string;
  environmentId?: string;
  // 前端已知的临时/本地变量覆盖
  variableOverrides?: Record<string, string>;
}

// 响应结果
interface ResponseResult {
  status: number;
  statusText: string;
  headers: KV[];
  cookies: Cookie[];
  body: ResponseBody;         // 小 body 内联，大 body 给 ref
  timing: Timing;
  sizeBytes: number;
  testResults: TestResult[];
  scriptLogs: string[];       // console.log 输出
}
interface Timing {
  dnsMs: number; connectMs: number; tlsMs: number;
  ttfbMs: number; downloadMs: number; totalMs: number;
}
```

`KV`/`Body`/`Auth` 等在前后端共享，是整个应用的稳定契约面。变更需走版本化（见下）。

---

## 4. Schema 迁移与版本兼容

- **数据库迁移**：`PRAGMA user_version` 记录版本；迁移 SQL 以内联、只追加的版本列表维护，启动时按版本顺序逐项补齐。每项迁移与版本更新在同一事务内执行，失败时回滚并中止启动。
- **导出格式版本**：内部模型带 `schemaVersion`；导入旧版本走升级适配器，导出默认最新、可选目标版本。
- **契约变更**：共享类型（本篇 §3）任何破坏性变更都要 bump 版本并提供前端兼容处理。
