<h1 align="center">ApiRequest</h1>

<p align="center">
  <b>调试任何 API — 原生网络能力 · 脚本就绪 · 告别 Electron 膨胀。</b>
</p>

<p align="center">
  基于 <a href="https://wails.io">Wails</a>（Go）+ <a href="https://react.dev">React</a> 的类 Postman API 客户端。
  请求在原生 Go 层执行 — 没有 CORS 限制，完整掌控 TLS 与代理，分阶段精确计时。
</p>

<p align="center">
  <a href="https://github.com/AutumnNazi/ApiRequest/stargazers"><img src="https://img.shields.io/github/stars/AutumnNazi/ApiRequest?style=for-the-badge&color=F59E0B" alt="Stars" /></a>
  <a href="https://github.com/AutumnNazi/ApiRequest/releases"><img src="https://img.shields.io/github/v/release/AutumnNazi/ApiRequest?style=for-the-badge&color=8B5CF6&include_prereleases" alt="Release" /></a>
</p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/github/go-mod/go-version/AutumnNazi/ApiRequest?style=flat-square&logo=go&logoColor=white&label=Go" alt="Go" /></a>
  <a href="https://wails.io"><img src="https://img.shields.io/badge/Wails-v2-red?style=flat-square" alt="Wails" /></a>
  <a href="https://reactjs.org"><img src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=white" alt="React" /></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript&logoColor=white" alt="TypeScript" /></a>
  <a href="https://github.com/AutumnNazi/ApiRequest/actions/workflows/dev-build.yml"><img src="https://img.shields.io/github/actions/workflow/status/AutumnNazi/ApiRequest/dev-build.yml?branch=dev&style=flat-square&label=Build" alt="Build" /></a>
</p>

<p align="center">
  <b>语言</b>：<a href="README.md">English</a> · 简体中文
  &nbsp;·&nbsp;
  <a href="#-快速开始"><b>⚡ 快速开始</b></a>
  ·
  <a href="#-核心能力"><b>✨ 特性</b></a>
  ·
  <a href="docs/index.md"><b>📚 设计文档</b></a>
</p>

---

## 为什么是 ApiRequest？

浏览器工具受制于 CORS，Electron 客户端捆着一整个 Chromium。ApiRequest 换了一条路：

| | 浏览器 / Electron 客户端 | **ApiRequest** |
|---|---|---|
| 请求引擎 | WebView `fetch`（受 CORS 限制）或 Node | **原生 Go `net/http`** |
| 体积 | 动辄数百 MB | **约 30MB 量级**（系统 WebView） |
| 计时 | 只有总耗时 | **DNS / 连接 / TLS / 首字节 / 下载** 分阶段（`httptrace`） |
| TLS 控制 | 有限 | **自定义 CA · mTLS 客户端证书 · 校验开关** |
| 脚本 | 参差不齐 | **沙箱 JS（goja），兼容 Postman `pm.*`** |
| 数据 | 云端优先 | **本地优先 SQLite，离线可用** |

> **构造 → 发送 → 断言 → Mock → 批量运行 — 一站式原生工作台。**
> HTTP、WebSocket、SSE、GraphQL；可从 Postman / OpenAPI / cURL / HAR / Insomnia 导入。

---

## 一图速览

```text
┌────────────────────────────────────────────────────────────────────┐
│  ApiRequest 工作台                                                  │
│  ┌─────────────┐  ┌───────────────────┐  ┌──────────────────────┐  │
│  │ 集合树       │  │ 请求编辑器         │  │ 响应查看器            │  │
│  │ 历史记录     │  │ Params · Headers  │  │ Pretty/Raw · 测试     │  │
│  │ 导入 / Run   │  │ Body · Auth       │  │ 计时瀑布              │  │
│  │ Mock        │  │ 脚本 · 设置        │  │ 保存为示例            │  │
│  └─────────────┘  └────────┬──────────┘  └──────────────────────┘  │
│                            │                                       │
│              ┌─────────────▼──────────────┐                        │
│              │  Go 核心 · net/http 引擎   │                        │
│              │  goja 沙箱 · SQLite        │                        │
│              │  Runner · Mock · WS/SSE    │                        │
│              └────────────────────────────┘                        │
└────────────────────────────────────────────────────────────────────┘
```

---

## ✨ 核心能力

<table>
<tr>
<td width="50%" valign="top">

### 🚀 原生请求引擎
- 全部 HTTP 方法 · 6 种 Body（raw / form-data / urlencoded / binary / GraphQL）
- 分阶段计时：DNS · 连接 · TLS · 首字节 · 下载
- 请求级重定向策略、超时、SSL 校验开关
- 大响应流式落盘 — 100MB 响应不卡 UI
- 持久化 Cookie Jar，自动携带与写回

</td>
<td width="50%" valign="top">

### 📜 熟悉的脚本体验
- 前置 / 测试脚本运行在沙箱 JS 引擎（goja，免 CGO）
- 兼容 Postman：`pm.environment` · `pm.test` · `pm.expect` · `pm.sendRequest`
- 集合级脚本与认证继承
- 多级变量作用域：全局 → 集合 → 环境 → 本地
- 动态变量：`{{$guid}}` `{{$timestamp}}` `{{$randomInt}}` …

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🔐 认证一应俱全
- Basic · Bearer · API Key · Digest（两段式）
- OAuth 1.0（HMAC 签名）· **OAuth 2.0**（授权码 + PKCE 本地回调、Client Credentials、Password、静默刷新）
- AWS Signature V4
- 自定义 CA（追加系统信任池）· mTLS 客户端证书
- 代理：系统 / 手动 / 直连

</td>
<td width="50%" valign="top">

### 🧰 不止于单个请求
- **Collection Runner**：CSV/JSON 数据驱动多轮、实时进度、报告可导出
- **Mock Server**：以"示例"为数据源，路径打分匹配 + `x-mock-response-*` 选择
- **WebSocket / SSE** 会话面板与消息时间线
- 导入：Postman v2.1 · OpenAPI 3 / Swagger 2 · cURL · HAR · Insomnia（自动识别）
- 导出 Postman v2.1 · 代码生成：cURL / fetch / Python / Go

</td>
</tr>
</table>

### 🧩 技术栈

`Go 1.26.5` · `Wails v2` · `React 18` · `TypeScript` · `Vite` · `Tailwind CSS` · `Zustand` · `TanStack Query` · `CodeMirror 6` · `goja` · `modernc.org/sqlite`（纯 Go 免 CGO）

---

## 🔌 协议与格式

| | |
|---|---|
| **协议** | HTTP/1.1 与 HTTP/2 · WebSocket · Server-Sent Events · GraphQL（query/mutation） |
| **导入** | Postman Collection v2.1 · OpenAPI 3.x（JSON/YAML）· Swagger 2 · cURL 命令 · HAR · Insomnia v4 |
| **导出** | Postman Collection v2.1 |
| **代码生成** | cURL · JavaScript (fetch) · Python (requests) · Go (net/http) |
| **存储** | 本地 SQLite 单文件 + 大响应 blob 目录 — 本地优先，离线可用 |

---

## 🚀 快速开始

### 环境要求

- [Go](https://go.dev/dl/) 1.26.5+
- [Node.js](https://nodejs.org/) 20.19+（Node 20）或 22.12+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 开发

```bash
git clone https://github.com/AutumnNazi/ApiRequest.git
cd ApiRequest

wails dev        # 前端 + Go 全热重载
```

### 构建

```bash
wails build      # 产物 → build/bin
```

### 测试

```bash
go test ./...                  # Go 核心与 Wails binding
cd frontend && npm test -- --run
cd frontend && npm run build   # 类型检查 + 打包
```

平台说明：Windows 需要 **WebView2 Runtime**（Win 11 / 较新的 Win 10 已预装）；macOS 与 Linux 使用系统 WebView（WebKit / WebKitGTK）。

### 下载

[正式版本](https://github.com/AutumnNazi/ApiRequest/releases/latest)与 [`dev-latest`](https://github.com/AutumnNazi/ApiRequest/releases/tag/dev-latest) 均发布两种受支持架构的原生包：

| 平台 | 架构 | 安装包 |
|------|------|--------|
| Windows | AMD64 | `ApiRequest-<版本>-Windows-Amd64-Installer.msi`、`-Portable.exe`、`-Portable.zip` |
| Windows | ARM64 | `ApiRequest-<版本>-Windows-Arm64-Installer.msi`、`-Portable.exe`、`-Portable.zip` |
| macOS | Intel | `ApiRequest-<版本>-MacOS-Amd64.dmg` |
| macOS | Apple Silicon | `ApiRequest-<版本>-MacOS-Arm64.dmg` |

Windows 常规安装请选择 MSI，无需安装时选择 Portable 包。Linux AMD64 作为额外的 best-effort 可执行文件发布。下载后应使用 `SHA256SUMS` 校验；运行开发版或未签名版本前，请查看对应的 `SIGNING_STATUS-*.txt`。

---

## 🗺 项目状态与路线图

核心闭环（构造 → 发送 → 查看 → 保存 → 重放）、变量与脚本、全部认证类型、导入导出、Runner、Mock、WS/SSE、gRPC、Git 友好集合镜像、WebDAV 同步和无头 CLI 运行器均**已实现并有测试覆盖**。近期还完成了 Secret Vault 凭据保护、工作区隔离会话、大响应有界渲染、SSE 断线续连和原生文件对话框。

完整功能清单与当前待办：[docs/roadmap.md](docs/roadmap.md) · 架构决策记录：[docs/decisions.md](docs/decisions.md)

---

## 📚 文档

设计文档在 [`docs/`](docs/) 目录，入口见 [docs/index.md](docs/index.md)：

- 初识项目 → [概览](docs/overview.md) → [路线图](docs/roadmap.md)
- 参与核心开发 → [数据模型](docs/data-model.md) → [请求生命周期](docs/request-lifecycle.md) → [接口约定](docs/api-contract.md)

---

## 🤝 参与贡献

欢迎 Issue 与 PR。**请勿直接向 `dev` 推送** — Fork 仓库后从 `dev` 创建分支（`fix/*` / `feature/*`），PR 提向 `dev`，合并采用 **Squash and merge**。

分支模型、PR 要求与发布流程见 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md)。提交 Bug 时请尽量附上版本、操作系统与复现步骤。

---

## 许可

待定。

<p align="center">
  <sub>献给整天与请求、Header 和状态码打交道的人。</sub>
</p>
