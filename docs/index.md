# ApiRequest 设计文档索引

[English](./en/index.md) | 简体中文

> 一个类 Postman 的 API 调试与协作工具。
> 技术选型：Wails v2（Go 后端）+ React + TypeScript（前端）。目标：完整版功能。

本目录是 ApiRequest 的设计文档集。项目总述见仓库根目录的 `README.md`；本页是设计文档的导航入口。

## 阅读路线

- 第一次了解项目：`overview.md` → `roadmap.md`
- 要动手实现核心闭环：`data-model.md` → `request-lifecycle.md` → `api-contract.md` → `roadmap.md`（Phase 1 任务分解）
- 关注 Windows / macOS 支持：`overview.md`（支持矩阵与 platform 抽象）→ `ops.md`（构建、签名与双端冒烟）→ `decisions.md`（密钥策略）
- 关注某一子系统：直接跳到对应文档

## 文档清单

| 文档 | 内容 |
|------|------|
| [overview.md](./overview.md) | 设计目标与原则、整体架构、技术栈 |
| [data-model.md](./data-model.md) | 领域模型、数据库 Schema、前后端共享类型、迁移与版本兼容 |
| [request-lifecycle.md](./request-lifecycle.md) | 请求生命周期、变量解析与模板引擎、脚本引擎执行模型、HTTP 引擎内核 |
| [auth.md](./auth.md) | 各认证类型实现细节、OAuth 2.0 时序 |
| [interop.md](./interop.md) | 导入/导出字段级映射、代码生成器架构 |
| [protocols.md](./protocols.md) | WebSocket / SSE / gRPC / GraphQL 多协议适配器 |
| [advanced.md](./advanced.md) | Mock Server、Collection Runner 执行引擎 |
| [frontend.md](./frontend.md) | 前端状态与数据流、UI/UX 交互规范 |
| [api-contract.md](./api-contract.md) | 前后端接口约定（Wails 绑定方法与 events） |
| [extensibility.md](./extensibility.md) | 可扩展性与插件接口 |
| [sync.md](./sync.md) | 协作与同步（可选叠加层） |
| [ops.md](./ops.md) | 安全、错误模型、测试策略、打包发布、性能预算 |
| [roadmap.md](./roadmap.md) | 功能清单、实施路线图、关键风险、Phase 1 任务分解 |
| [decisions.md](./decisions.md) | 架构决策记录（ADR）与待拍板的开放问题 |
| [glossary.md](./glossary.md) | 术语表 |

## 目录结构（建议）

完整目录结构以 [overview.md](./overview.md#目录结构建议) 为唯一维护点，此处不再重复（要点：前端在 `frontend/`，Go 后端在 `backend/` 下按 binding / httpengine / script / template / storage / platform / convert / protocol / mock / model 分包，`wailsjs/` 由 Wails 生成不手写）。
