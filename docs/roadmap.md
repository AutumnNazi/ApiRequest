# 功能清单、路线图与风险

> 完整版功能清单、分阶段实施路线、关键技术风险，以及 Phase 1 的可开工任务分解。
> 返回 [文档索引](./index.md)。

---

## 1. 功能清单（完整版）

### 请求构造
- [ ] HTTP 方法全集（GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS + 自定义）
- [ ] URL 与 Query 参数编辑（双向同步）
- [ ] Header 编辑（含自动填充、批量编辑、禁用项）
- [ ] Body：form-data / x-www-form-urlencoded / raw(JSON/XML/HTML/Text) / binary / GraphQL
- [ ] 认证：No Auth / Basic / Bearer / API Key / Digest / OAuth 1.0 / OAuth 2.0 / AWS Signature
- [ ] 请求级设置覆盖：超时、重定向、SSL 校验、编码

### 变量与环境
- [ ] 全局 / 集合 / 环境 / 本地多级变量
- [ ] 环境切换与快速编辑
- [ ] 动态变量（`{{$guid}}`、`{{$timestamp}}`、`{{$randomInt}}` 等）
- [ ] 变量引用高亮与未定义提示

### 脚本
- [ ] 前置脚本（Pre-request）
- [ ] 测试脚本（Tests）与断言
- [ ] 集合/文件夹级脚本继承
- [ ] `pm.*` API 兼容子集
- [ ] 脚本内 `sendRequest`

### 响应
- [ ] Body 视图：Pretty(JSON/XML 折叠) / Raw / Preview(HTML/图片)
- [ ] Header / Cookie / 测试结果 标签页
- [ ] 状态码、耗时、大小、分阶段计时
- [ ] 大响应流式加载 + 搜索
- [ ] 保存为示例（Example）

### 组织与管理
- [ ] 工作区（Workspace）
- [ ] 集合 / 嵌套文件夹 / 请求树
- [ ] 多标签页编辑
- [ ] 历史记录（可搜索、可重放）
- [ ] Cookie 管理器（Cookie Jar 查看编辑）

### 协议扩展
- [ ] WebSocket
- [ ] Server-Sent Events (SSE)
- [ ] GraphQL（schema 内省 + 补全）
- [ ] gRPC（后期）

### 互操作
- [ ] 导入：Postman v2.1 / OpenAPI / Swagger / cURL / HAR / Insomnia
- [ ] 导出：Postman v2.1 / OpenAPI / cURL
- [ ] 代码生成：多语言片段
- [ ] 集合的 Git 友好文件镜像

### 高级
- [ ] Mock Server
- [ ] Collection Runner（数据文件驱动、运行报告）
- [ ] 代理设置（系统/自定义/绕过列表）
- [ ] 自定义 TLS / 客户端证书
- [ ] 团队协作同步（可选）

---

## 2. 实施路线图（分阶段）

**Phase 1 — 骨架与核心请求（MVP 基线）**
Wails v2 + React 脚手架 → 数据模型与 SQLite → HTTP 引擎(SendRequest) → 请求编辑器 + 响应查看器 → 集合树 + 历史。

**Phase 2 — 变量与脚本**
多级变量与环境 → 模板解析 → goja 脚本引擎 + `pm.*` → 测试结果面板。

**Phase 3 — 认证与互操作**
全部 auth 类型 → 导入导出转换器 → 代码生成 → Cookie 管理。

**Phase 4 — 高级能力**
Collection Runner → Mock Server → WebSocket/SSE/GraphQL。

**Phase 5 — 协作与打磨**
同步层 → gRPC → 性能优化、快捷键、主题、可访问性。

---

## 3. 关键技术风险

| 风险 | 说明 | 应对 |
|------|------|------|
| 脚本引擎兼容度 | Postman `pm.*` API 面很大，全兼容成本高 | 先覆盖高频子集，按需扩展；文档标注已支持范围 |
| 大响应性能 | 几十 MB 响应体渲染卡顿 | 流式 + 虚拟化 + 折叠懒渲染，超阈值仅显示摘要 |
| OAuth 2.0 流程 | 授权码模式需拉起浏览器与回调 | 经 platform.open 打开系统浏览器 + 本地回调端口接收 |
| 原始 Header 顺序/大小写 | Go `net/http` 的 Header 是 map、不保序 | 需要时自定义保序结构，保留原始顺序 |
| 跨平台一致性 | Windows/macOS/Linux WebView 差异 | CI 三平台构建 + 冒烟测试 |

---

## 4. Phase 1 任务分解（可直接开工）

目标：跑通"建请求 → 发送 → 看响应 → 存集合 → 看历史"的最小闭环。

**后端（Go/Wails）**
1. Wails v2 工程初始化，接入 goroutine + `context`、`net/http`（标准库，内建 gzip、TLS）、`modernc.org/sqlite`（纯 Go，免 CGO）、`encoding/json`、goja（Phase 2 用）。
2. `storage`：基于 `modernc.org/sqlite` 建库 + `PRAGMA user_version` 迁移器；`workspace`/`node`/`history` 三表落地。
3. `model` + Wails `wails generate module` 生成 TS 绑定/类型到 `frontend/wailsjs/`。
4. `httpengine`：`SendRequest` 最小实现（method/url/headers/body/基础计时），`context.Context` 支持取消。
5. Wails 绑定方法：`SendRequest` / `ListNodes` / `UpsertNode` / `ListHistory`。
6. `request:progress` Wails 事件（`runtime.EventsEmit`）通道打通。

**前端（frontend/src）**
7. Vite + React + TS + Tailwind + Radix 脚手架；`ipc/` typed wrapper 骨架。
8. 三栏布局：左侧集合树、中间请求编辑器、右侧/下方响应查看器。
9. 请求编辑器：method 选择、URL 输入、Header/Query 表格、Body(raw JSON) 编辑（CodeMirror 6）。
10. 响应查看器：状态/耗时/大小、Pretty JSON、Header 标签页。
11. 集合树 CRUD（建/删/重命名/拖拽排序）+ 多标签页。
12. 历史列表（虚拟列表）+ 点击重放。

**验收标准**：对任意公开 API 发 GET/POST，正确显示响应与计时；请求可存入集合并重开；历史可重放。Windows 10/11 x64 与 macOS 12+（Apple Silicon、Intel）构建产物均可启动并跑通一次本地 mock 请求，数据落在 Wails 运行时路径 + Go `path/filepath` 解析的应用数据目录。**里程碑之间保持可编译、可运行、关键路径有测试**。
