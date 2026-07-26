# ApiRequest

一个类 Postman 的 API 调试与协作工具。请求执行下沉到 Go 原生层，绕开浏览器限制，可完整控制网络、脚本与本地数据。

- **形态**：跨平台桌面应用（Windows / macOS / Linux）
- **技术栈**：Wails v2（Go 后端）+ React 18 + TypeScript（前端）
- **目标**：完整版功能，对齐主流 API 工具能力

---

## 为什么用它

- **原生网络能力**：HTTP 请求在 Go 侧用 `net/http` 执行，不受 WebView 的 CORS 与同源限制，可自定义 TLS、客户端证书、代理、重定向、原始 Header 顺序，并用标准库 `net/http/httptrace` 采集分阶段计时。
- **数据本地优先**：集合、环境、历史存于本地 SQLite，离线可用；协作同步为可选叠加层。
- **脚本与测试**：前置脚本 / 测试脚本运行在沙箱化 JS 引擎中，兼容 Postman `pm.*` 常用 API 子集。
- **可迁移**：支持导入 / 导出 Postman、OpenAPI、cURL、HAR 等格式，以及 Git 友好的集合文件镜像。

## 核心能力

请求构造（全 HTTP 方法 / 多种 Body / 9 类认证）、多级变量与环境、前置与测试脚本、Collection Runner（数据驱动 + 报告）、Mock Server、多协议（WebSocket / SSE / gRPC / GraphQL）、代码生成、导入导出、可选团队协作同步。

完整功能清单见 [`docs/roadmap.md`](./docs/roadmap.md)。

## 项目状态

设计阶段。架构与实现方案已成文，代码尚未开始。实施按阶段推进，详见 [`docs/roadmap.md`](./docs/roadmap.md)（Phase 1 为"建请求 → 发送 → 看响应 → 存集合 → 看历史"的最小闭环）。

## 文档

所有设计文档在 [`docs/`](./docs/) 目录，导航入口见 [`docs/index.md`](./docs/index.md)。

- 第一次了解项目：[`docs/overview.md`](./docs/overview.md) → [`docs/roadmap.md`](./docs/roadmap.md)
- 动手实现核心闭环：[`docs/data-model.md`](./docs/data-model.md) → [`docs/request-lifecycle.md`](./docs/request-lifecycle.md) → [`docs/api-contract.md`](./docs/api-contract.md)

## 目录结构

```
ApiRequest/
├── frontend/      # React 前端（src: components / features / stores / ipc / types；wailsjs: Wails 生成的绑定）
├── backend/       # Go 后端（binding / httpengine / script / template / storage / platform / convert / protocol / mock / model）
├── app.go         # Wails App 结构体（绑定入口）
├── main.go        # Wails 应用启动入口
├── wails.json     # Wails 项目配置
├── go.mod         # Go 模块定义
├── docs/          # 设计文档
└── README.md      # 本文件
```

跨平台支持矩阵、打包签名与 CI 冒烟见 [`docs/ops.md`](./docs/ops.md) 与 [`docs/overview.md`](./docs/overview.md)。

## 许可

待定。
