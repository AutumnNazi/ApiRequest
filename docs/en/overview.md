# Overview: Goals, Architecture, and Technology Stack

English | [简体中文](../overview.md)

> A Postman-like API debugging and collaboration tool.
> Stack: Wails v2 (Go backend) + React + TypeScript (frontend). Goal: a full-featured release.

Related: [Documentation Index](./index.md) · [Data Model](./data-model.md) · [Request Lifecycle](./request-lifecycle.md)

---

## 1. Design Goals and Principles

| Goal | Description |
|------|-------------|
| Native performance and capabilities | Execute HTTP requests in Go to bypass browser CORS and fully control TLS, proxies, cookies, timeouts, and redirects |
| First-class cross-platform support | Windows and macOS are release targets; Linux builds in parallel on a best-effort basis. Platform differences are isolated behind the Go `platform` abstraction, and the UI never calls OS APIs directly |
| Local-first data | Store collections, environments, and history locally for offline use; collaboration sync is an optional layer |
| Extensibility | Organize request protocols (HTTP/WS/SSE/gRPC), import/export formats, and code-generation languages behind plugin-style interfaces |
| Scripting | Run pre-request and test scripts in a sandboxed JavaScript engine compatible with commonly used Postman `pm.*` APIs |
| Testability | Keep core logic such as variable resolution, request construction, and script execution in Go and pure-function layers that can be tested without the UI |

**Layering principle**: the UI layer handles presentation and interaction, not business logic. Every operation that can fail, including network, file, and script execution, runs in the Go core and is exposed through Wails bindings. OS-specific capabilities such as paths, secrets, system proxies, and certificate stores must go through the `platform` package; business code must not contain ad hoc platform branches.

### 1.1 Platform Support Matrix

| Platform | Support | Minimum OS | Architectures | Runtime Dependency |
|----------|---------|------------|---------------|--------------------|
| Windows | **First-class** | Windows 10 1809+ / Windows 11 | x64; arm64 later | WebView2 Runtime, with installer guidance when needed |
| macOS | **First-class** | macOS 12 Monterey+ | Apple Silicon + Intel, universal or separate artifacts | System WKWebView |
| Linux | Best-effort | Common distributions such as Ubuntu 22.04+ | x64 | WebKitGTK |

Wails uses the system WebView (WebView2 on Windows, WKWebView on macOS, and WebKitGTK on Linux), so no browser engine is bundled. See [ops.md](./ops.md#4-cross-platform-support-and-releases) for cross-platform packaging, signing, CI, and smoke-test gates.

---

## 2. Overall Architecture

```text
┌───────────────────────────────────────────────────────────────┐
│                     Frontend (WebView)                         │
│  React + TypeScript + Vite                                     │
│                                                                │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────────┐    │
│  │ UI Layer    │  │ State Layer  │  │ Typed IPC Client   │    │
│  │ views/editors│ │ Zustand +    │  │ invoke / event     │    │
│  │             │  │ TanStack Q   │  │ wrappers           │    │
│  └─────────────┘  └──────────────┘  └────────────────────┘    │
└───────────────────────────────┬───────────────────────────────┘
                                │ Wails bindings (calls/events)
┌───────────────────────────────┴───────────────────────────────┐
│                    Backend (Go Core)                           │
│                                                                │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌─────────────┐ │
│  │ Binding    │ │ HTTP Engine│ │ Script     │ │ Variables / │ │
│  │ API boundary││ net/http + │ │ goja       │ │ Templates   │ │
│  │            │ │ httptrace  │ │            │ │             │ │
│  └────────────┘ └────────────┘ └────────────┘ └─────────────┘ │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌─────────────┐ │
│  │ Storage    │ │ Import /   │ │ Mock Server│ │ Protocols   │ │
│  │ SQLite     │ │ Export     │ │ net/http   │ │ WS/SSE/gRPC │ │
│  └────────────┘ └────────────┘ └────────────┘ └─────────────┘ │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │ platform abstraction: paths / secrets / proxy / certs / open│
│  └──────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
                                │
                    ┌───────────┴───────────┐
                    │ Optional Sync Service │
                    │ remote API + merging  │
                    └───────────────────────┘
```

### 2.1 Why Requests Run in Go

- **Bypass CORS**: `fetch` inside a WebView is constrained by same-origin policy, but a Postman-like tool must call arbitrary targets. Go's `net/http` sends native requests directly.
- **Full network control**: custom CAs and client certificates, proxy behavior, redirect policy, original header order, and timeouts.
- **Accurate timing**: collect DNS, connection, TLS handshake, TTFB, and download timing in the native layer through the standard `net/http/httptrace` package.
- **Stream large responses**: avoid placing a complete large body in the WebView at once.

See [Request Lifecycle and HTTP Engine](./request-lifecycle.md).

---

## 3. Technology Stack

### Frontend

| Area | Choice | Rationale |
|------|--------|-----------|
| Framework | React 18 + TypeScript | Broad ecosystem and component availability |
| Build | Vite | Official Wails support and fast HMR |
| State management | Zustand | Lightweight and suitable for local UI state such as open tabs and panel layout |
| Server/async state | TanStack Query | Caching and invalidation for data exchanged with the Go core |
| Routing | React Router | Multi-workspace and multi-tab navigation |
| Code editor | Monaco or CodeMirror 6 | JSON/script editing, syntax highlighting, and completion; CM6 is lighter and preferred |
| UI components | Radix UI + Tailwind CSS | Accessible unstyled primitives plus controllable utility styling |
| Virtual lists | TanStack Virtual | Large history lists and response bodies |

### Backend (Go)

| Area | Choice |
|------|--------|
| Application framework | Wails v2 |
| HTTP client | Standard `net/http`, with `crypto/tls` for TLS, `net/http/cookiejar` for cookies, and gzip/br/deflate decoding |
| Concurrency | goroutine + `context`; cancellation and timeout use `context.Context`, with no separate async runtime |
| JS engine | goja, a pure-Go ES5.1+ implementation with sandboxing and fast startup, without CGO |
| Local storage | SQLite through pure-Go `modernc.org/sqlite` for straightforward cross-compilation |
| Secret storage | `zalando/go-keyring` for the system keychain, with master-password encryption fallback |
| Mock/local services | Standard `net/http`, optionally with `go-chi/chi` routing |
| Serialization | Standard `encoding/json` |
| gRPC | `google.golang.org/grpc` + `protoreflect`/`dynamicpb` for optional dynamic invocation |
| Shared types | Built-in Wails Go-to-TypeScript binding generation, with no handwritten generation pipeline |

---

## 4. The `platform` Abstraction

Business modules such as `http`, `storage`, and `auth` depend **only on the following unified interfaces**. Platform-specific implementations use Go build tags (`//go:build windows` / `//go:build darwin`) or `runtime.GOOS` branches contained under `backend/platform/`:

| Module | Responsibility | Windows | macOS |
|--------|----------------|---------|-------|
| `paths` | Application data, logs, and blobs root | `%APPDATA%\com.apirequest.app\` through `os.UserConfigDir` | `~/Library/Application Support/com.apirequest.app/` |
| `secrets` | Secret variables and OAuth token storage | Credential Manager through `go-keyring` | Keychain through `go-keyring` |
| `proxy` | Read system proxy and bypass rules | WinHTTP / registry; fall back to `http.ProxyFromEnvironment` | System Configuration; fall back to environment variables |
| `certs` | Load custom CAs and client certificates | Files plus optional system certificate store | Files plus Keychain trust |
| `open` | Open the system browser for OAuth and similar flows | System default through Wails `runtime.BrowserOpenURL` | Same |

Conventions:

- **Paths**: always use standard APIs such as `path/filepath` and `os.UserConfigDir`; never concatenate path separators. Relative paths persisted to the database or mirror always use `/`.
- **Secrets**: see [ADR-013](./decisions.md#adr-013-use-the-system-keychain-by-default-with-master-password-encryption-as-fallback-accepted).
- **Proxy/certificates**: inject configuration into the HTTP engine through `platform`; the UI only displays results and manual overrides.

See [ops.md](./ops.md#4-cross-platform-support-and-releases) for packaging artifacts, the CI matrix, and smoke-test coverage.

---

## Suggested Directory Structure

```text
ApiRequest/
├── frontend/                   # React frontend (Wails frontend directory)
│   ├── src/
│   │   ├── components/         # UI components
│   │   ├── features/           # Feature domains: request/collection/env/runner/...
│   │   ├── stores/             # Zustand stores
│   │   ├── ipc/                # Typed wrappers around Wails bindings
│   │   └── types/              # Shared types: Wails-generated + handwritten additions
│   └── wailsjs/                # Wails-generated Go-to-JS bindings and TS types
├── backend/                    # Go backend
│   ├── binding/                # Wails binding layer and frontend API boundary
│   ├── httpengine/             # net/http + httptrace wrapper and timings
│   ├── script/                 # goja script engine + pm API
│   ├── template/               # Variable resolution and template rendering
│   ├── storage/                # SQLite access through modernc.org/sqlite
│   ├── platform/               # Cross-platform paths/secrets/proxy/certs/open
│   ├── convert/                # Import/export converters
│   ├── protocol/               # WS/SSE/gRPC adapters
│   ├── mock/                   # Mock server through net/http
│   └── model/                  # Domain model
├── docs/                       # Design documentation
├── app.go                      # Wails App and binding entry point
├── main.go                     # Wails application entry point
├── wails.json                  # Wails project configuration
└── go.mod
```

> This layout follows Wails conventions: the frontend is under `frontend/`, Go backend packages live under `backend/`, and `wailsjs/` is generated rather than edited manually.
