# 概览：目标、架构与技术栈

[English](./en/overview.md) | 简体中文

> 一个类 Postman 的 API 调试与协作工具。
> 技术选型：Wails v2（Go 后端）+ React + TypeScript（前端）。目标：完整版功能。

相关文档：[文档索引](./index.md) · [数据模型](./data-model.md) · [请求生命周期](./request-lifecycle.md)

---

## 1. 设计目标与原则

| 目标 | 说明 |
|------|------|
| 原生性能与能力 | HTTP 请求在 Go 侧执行，绕开浏览器 CORS，可完整控制 TLS、代理、Cookie、超时、重定向 |
| 跨平台一等支持 | Windows 与 macOS 为正式交付目标（Linux 同步构建、best-effort）；平台差异收敛在 Go `platform` 抽象层，UI 不直接依赖 OS API |
| 数据本地优先 | 集合/环境/历史存本地，可离线使用；协作同步为可选叠加层 |
| 可扩展 | 请求协议（HTTP/WS/SSE/gRPC）、导入导出格式、代码生成语言均以插件式接口组织 |
| 脚本能力 | 前置脚本 / 测试脚本使用沙箱化 JS 引擎，兼容 Postman `pm.*` 常用 API |
| 可测试 | 核心逻辑（变量解析、请求构造、脚本运行）在 Go/纯函数层，脱离 UI 可单测 |

**架构分层原则**：UI 层只做展示与交互，不含业务逻辑；所有"会失败"的操作（网络、文件、脚本执行）下沉到 Go core，通过 Wails 绑定方法暴露给前端。涉及 OS 差异的能力（路径、密钥、系统代理、证书库）一律经 `platform` 包访问，禁止业务代码手写平台分支。

### 1.1 平台支持矩阵

| 平台 | 支持级别 | 最低系统 | 架构 | 运行时依赖 |
|------|----------|----------|------|------------|
| Windows | **一等** | Windows 10 1809+ / Windows 11 | x64（arm64 后期） | WebView2 Runtime（安装包可引导安装） |
| macOS | **一等** | macOS 12 Monterey+ | Apple Silicon + Intel（universal 或分架构产物） | 系统 WKWebView |
| Linux | best-effort | 常见发行版（Ubuntu 22.04+ 等） | x64 | WebKitGTK |

Wails 使用系统 WebView（Windows WebView2 / macOS WKWebView / Linux WebKitGTK），无需捆绑浏览器内核。跨平台打包、签名、CI 与冒烟门禁见 [ops.md](./ops.md#4-跨平台支持与发布)。

---

## 2. 整体架构

```
┌───────────────────────────────────────────────────────────────┐
│                     Frontend (WebView)                          │
│  React + TypeScript + Vite                                      │
│                                                                 │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────────┐     │
│  │  UI 组件层   │  │  状态管理层   │  │  IPC 客户端封装      │     │
│  │ (视图/编辑器)│  │ (Zustand +    │  │ (invoke / event    │     │
│  │             │  │  TanStack Q)  │  │  的 typed wrapper)  │     │
│  └─────────────┘  └──────────────┘  └────────────────────┘     │
└───────────────────────────────┬───────────────────────────────┘
                                 │  Wails Binding (方法调用 / 事件)
┌───────────────────────────────┴───────────────────────────────┐
│                    Backend (Go Core)                            │
│                                                                 │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌─────────────┐  │
│  │ Binding 层  │ │ HTTP 引擎  │ │ 脚本引擎    │ │ 变量/模板   │  │
│  │ (API 边界)  │ │(net/http +  │ │  (goja)    │ │  解析器      │  │
│  │            │ │ httptrace)  │ │            │ │             │  │
│  └────────────┘ └────────────┘ └────────────┘ └─────────────┘  │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌─────────────┐  │
│  │ 存储层      │ │ 导入导出    │ │ Mock Server │ │ 协议适配器   │  │
│  │ (SQLite)   │ │ (转换器)    │ │(net/http)  │ │ WS/SSE/gRPC │  │
│  └────────────┘ └────────────┘ └────────────┘ └─────────────┘  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ platform 抽象：paths / secrets / proxy / certs / open     │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                                 │
                    ┌────────────┴────────────┐
                    │  可选：协作同步服务       │
                    │  (远程 API + 冲突合并)    │
                    └─────────────────────────┘
```

### 2.1 为什么请求执行放在 Go 侧

- **绕开 CORS**：WebView 里 `fetch` 受同源策略限制，Postman 类工具必须能请求任意目标。放到 Go 用 `net/http` 直接发原生请求。
- **完整网络控制**：自定义 TLS 证书、客户端证书、代理、重定向策略、原始 Header 顺序、超时。
- **精确计时**：DNS / 连接 / TLS 握手 / TTFB / 下载各阶段耗时，用标准库 `net/http/httptrace` 在原生层采集。
- **大响应流式处理**：避免把大 body 一次性塞进 WebView。

详见 [请求生命周期与 HTTP 引擎](./request-lifecycle.md)。

---

## 3. 技术栈

### 前端
| 领域 | 选型 | 理由 |
|------|------|------|
| 框架 | React 18 + TypeScript | 生态最丰富，组件库多 |
| 构建 | Vite | Wails 官方支持，HMR 快 |
| 状态管理 | Zustand | 轻量，适合本地 UI 状态（打开的标签页、面板布局） |
| 服务端/异步状态 | TanStack Query | 管理与 Go core 交互的数据缓存与失效 |
| 路由 | React Router | 多工作区/标签页导航 |
| 代码编辑器 | Monaco 或 CodeMirror 6 | JSON/脚本编辑、语法高亮、补全（CM6 更轻，推荐） |
| UI 组件 | Radix UI + Tailwind CSS | 无样式可访问性组件 + 原子化样式，可控 |
| 虚拟列表 | TanStack Virtual | 大历史记录/大响应体渲染 |

### 后端（Go）
| 领域 | 选型 |
|------|------|
| 应用框架 | Wails v2 |
| HTTP 客户端 | 标准库 `net/http`（`crypto/tls` 配置 TLS、`net/http/cookiejar` 管理 Cookie；gzip/br/deflate 解码） |
| 并发模型 | goroutine + `context`（取消/超时用 `context.Context`，无需独立异步运行时） |
| JS 脚本引擎 | goja（纯 Go ES5.1+ 实现，无 CGO，可沙箱、启动快） |
| 本地存储 | SQLite（`modernc.org/sqlite`，纯 Go 免 CGO，利于交叉编译） |
| 密钥存储 | `zalando/go-keyring`（系统钥匙串）+ 主密码加密回退 |
| Mock/本地服务 | 标准库 `net/http`（可配 `go-chi/chi` 路由） |
| 序列化 | 标准库 `encoding/json` |
| gRPC | `google.golang.org/grpc` + `protoreflect`/`dynamicpb`（动态调用，可选特性） |
| 类型共享 | Wails 内置 Go→TS 绑定生成（免手写、免额外流水线） |

---

## 4. 平台抽象层（`platform`）

业务模块（`http` / `storage` / `auth` 等）**只依赖下列统一接口**，具体实现按 Go build tags（`//go:build windows` / `//go:build darwin`）或 `runtime.GOOS` 分支，集中在 `backend/platform/`：

| 子模块 | 职责 | Windows | macOS |
|--------|------|---------|-------|
| `paths` | 应用数据目录、日志目录、blobs 根路径 | `%APPDATA%\com.apirequest.app\`（经 `os.UserConfigDir`） | `~/Library/Application Support/com.apirequest.app/` |
| `secrets` | 密钥变量 / OAuth token 存取 | Credential Manager（`go-keyring`） | Keychain（`go-keyring`） |
| `proxy` | 读取系统代理与绕过列表 | WinHTTP / 注册表；失败则 `http.ProxyFromEnvironment` | System Configuration；失败则环境变量 |
| `certs` | 自定义 CA、客户端证书加载 | 文件 + 可选系统证书库 | 文件 + 钥匙串信任 |
| `open` | 打开系统浏览器（OAuth）等 | 系统默认关联（Wails `runtime.BrowserOpenURL`） | 同上 |

约定：
- **路径**：一律用 `path/filepath` 与 `os.UserConfigDir` 等标准库 API，禁止字符串拼接分隔符；持久化到库/镜像的相对路径统一用 `/`。
- **密钥**：见 [ADR-013](./decisions.md#adr-013-密钥存储默认系统-keychain无可用时主密码加密回退已定)。
- **代理 / 证书**：HTTP 引擎通过 `platform` 注入配置，UI 只展示结果与手动覆盖项。

细节（打包产物、CI 矩阵、冒烟清单）见 [ops.md](./ops.md#4-跨平台支持与发布)。

---

## 目录结构（建议）

```
ApiRequest/
├── frontend/                   # React 前端（Wails 前端目录）
│   ├── src/
│   │   ├── components/         # UI 组件
│   │   ├── features/           # 按功能域组织（request/collection/env/runner...）
│   │   ├── stores/             # Zustand stores
│   │   ├── ipc/                # Wails 绑定的 typed 封装
│   │   └── types/              # 与 Go 共享的类型（Wails 生成 + 手写补充）
│   └── wailsjs/                # Wails 自动生成的 Go→JS 绑定与 TS 类型
├── backend/                    # Go 后端
│   ├── binding/                # Wails 绑定层（导出给前端的方法，API 边界）
│   ├── httpengine/             # HTTP 引擎（net/http + httptrace 封装、计时）
│   ├── script/                 # goja 脚本引擎 + pm API
│   ├── template/               # 变量解析/模板渲染
│   ├── storage/                # SQLite 访问层（modernc.org/sqlite）
│   ├── platform/               # 跨平台抽象：paths / secrets / proxy / certs / open
│   ├── convert/                # 导入导出转换器
│   ├── protocol/               # WS/SSE/gRPC 适配器
│   ├── mock/                   # Mock server (net/http)
│   └── model/                  # 领域模型
├── docs/                       # 设计文档
├── app.go                      # Wails App 结构体（绑定入口）
├── main.go                     # Wails 应用启动入口
├── wails.json                  # Wails 项目配置
└── go.mod
```

> 目录布局遵循 Wails 约定：前端在 `frontend/`，Go 后端代码在根模块下按包组织，`wailsjs/` 由 Wails 生成、不手写。
