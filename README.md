<h1 align="center">ApiRequest</h1>

<p align="center">
  <b>Debug any API — native networking, script-ready, zero Electron bloat.</b>
</p>

<p align="center">
  A Postman-class API client built with
  <a href="https://wails.io">Wails</a> (Go) + <a href="https://react.dev">React</a>.
  Requests run in native Go — no CORS walls, full TLS/proxy control, per-phase timing.
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
</p>

<p align="center">
  <b>Language</b>: English · <a href="README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="#-quick-start"><b>⚡ Quick Start</b></a>
  ·
  <a href="#-key-features"><b>✨ Features</b></a>
  ·
  <a href="docs/index.md"><b>📚 Design Docs</b></a>
</p>

---

## Why ApiRequest?

Browser-based tools hit CORS walls; Electron clients ship a whole Chromium. ApiRequest takes a different path:

| | Browser / Electron client | **ApiRequest** |
|---|---|---|
| Request engine | WebView `fetch` (CORS-bound) or Node | **Native Go `net/http`** |
| Binary size | Hundreds of MB | **~30MB class** (system WebView) |
| Timing | Total only | **DNS / Connect / TLS / TTFB / Download** via `httptrace` |
| TLS control | Limited | **Custom CA · mTLS client certs · verify toggle** |
| Scripting | Varies | **Sandboxed JS (goja), Postman `pm.*` compatible** |
| Data | Cloud-first | **Local-first SQLite, offline-ready** |

> **Build → send → assert → mock → run — one native cockpit.**
> HTTP, WebSocket, SSE, GraphQL. Import from Postman / OpenAPI / cURL / HAR / Insomnia.

---

## At a Glance

```text
┌────────────────────────────────────────────────────────────────────┐
│  ApiRequest Workbench                                              │
│  ┌─────────────┐  ┌───────────────────┐  ┌──────────────────────┐  │
│  │ Collections │  │ Request Editor    │  │ Response Viewer      │  │
│  │ History     │  │ Params · Headers  │  │ Pretty/Raw · Tests   │  │
│  │ Import/Run  │  │ Body · Auth       │  │ Timing waterfall     │  │
│  │ Mock        │  │ Scripts · Config  │  │ Save as Example      │  │
│  └─────────────┘  └────────┬──────────┘  └──────────────────────┘  │
│                            │                                       │
│              ┌─────────────▼──────────────┐                        │
│              │  Go core · net/http engine │                        │
│              │  goja sandbox · SQLite     │                        │
│              │  Runner · Mock · WS/SSE    │                        │
│              └────────────────────────────┘                        │
└────────────────────────────────────────────────────────────────────┘
```

---

## ✨ Key Features

<table>
<tr>
<td width="50%" valign="top">

### 🚀 Native request engine
- All HTTP methods · 6 body types (raw / form-data / urlencoded / binary / GraphQL)
- Per-phase timing: DNS · connect · TLS · TTFB · download
- Redirect policy, timeout, SSL-verify per request
- Large responses stream to disk — 100MB body won't freeze the UI
- Persistent Cookie Jar, auto attach & capture

</td>
<td width="50%" valign="top">

### 📜 Scripts that feel like home
- Pre-request & test scripts in a sandboxed JS engine (goja, no CGO)
- Postman-compatible: `pm.environment` · `pm.test` · `pm.expect` · `pm.sendRequest`
- Collection-level script & auth inheritance
- Variable scopes: global → collection → environment → local
- Dynamic vars: `{{$guid}}` `{{$timestamp}}` `{{$randomInt}}` …

</td>
</tr>
<tr>
<td width="50%" valign="top">

### 🔐 Auth, all of it
- Basic · Bearer · API Key · Digest (two-phase)
- OAuth 1.0 (HMAC signing) · **OAuth 2.0** (Auth Code + PKCE with local callback, Client Credentials, Password, silent refresh)
- AWS Signature V4
- Custom CA (appended to system pool) · mTLS client certificates
- Proxy: system / manual / direct

</td>
<td width="50%" valign="top">

### 🧰 Beyond a single request
- **Collection Runner**: CSV/JSON data-driven iterations, live progress, exportable report
- **Mock Server**: serve saved Examples with path scoring & `x-mock-response-*` selection
- **WebSocket / SSE** session panel with message timeline
- Import: Postman v2.1 · OpenAPI 3 / Swagger 2 · cURL · HAR · Insomnia (auto-detect)
- Export Postman v2.1 · Codegen: cURL / fetch / Python / Go

</td>
</tr>
</table>

### 🧩 Stack

`Go 1.23` · `Wails v2` · `React 18` · `TypeScript` · `Vite` · `Tailwind CSS` · `Zustand` · `TanStack Query` · `CodeMirror 6` · `goja` · `modernc.org/sqlite` (pure Go, no CGO)

---

## 🔌 Protocols & Formats

| | |
|---|---|
| **Protocols** | HTTP/1.1 & HTTP/2 · WebSocket · Server-Sent Events · GraphQL (query/mutation) |
| **Import** | Postman Collection v2.1 · OpenAPI 3.x (JSON/YAML) · Swagger 2 · cURL command · HAR · Insomnia v4 |
| **Export** | Postman Collection v2.1 |
| **Codegen** | cURL · JavaScript (fetch) · Python (requests) · Go (net/http) |
| **Storage** | Local SQLite (single file) + blob store for large bodies — local-first, offline-ready |

---

## 🚀 Quick Start

### Prerequisites

- [Go](https://go.dev/dl/) 1.23+
- [Node.js](https://nodejs.org/) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### Develop

```bash
git clone https://github.com/AutumnNazi/ApiRequest.git
cd ApiRequest

wails dev        # hot reload for frontend + Go
```

### Build

```bash
wails build      # artifacts → build/bin
```

### Test

```bash
go test ./backend/...          # Go core: engine, scripts, storage, converters
cd frontend && npm run build   # type-check + bundle
```

Platform notes: Windows needs the **WebView2 Runtime** (preinstalled on Win 11 / recent Win 10); macOS and Linux use the system WebView (WebKit / WebKitGTK).

---

## 🗺 Status & Roadmap

Core loop (build → send → inspect → save → replay), variables & scripts, all auth types, import/export, Runner, Mock, WS/SSE are **implemented and tested**. Upcoming: gRPC, Git-friendly collection mirror, response preview polish, optional team sync, CLI runner.

Full checklist and phase plan: [docs/roadmap.md](docs/roadmap.md) · Architecture decisions: [docs/decisions.md](docs/decisions.md)

---

## 📚 Documentation

Design docs live in [`docs/`](docs/) — start at [docs/index.md](docs/index.md):

- New to the project → [overview](docs/overview.md) → [roadmap](docs/roadmap.md)
- Hacking on core → [data model](docs/data-model.md) → [request lifecycle](docs/request-lifecycle.md) → [API contract](docs/api-contract.md)

---

## 🤝 Contributing

Issues and PRs welcome. Branch from **`dev`**, PR against **`dev`**.

Please include version, OS, and repro steps in bug reports.

---

## License

To be determined.

<p align="center">
  <sub>Built for people who live in requests, headers, and status codes.</sub>
</p>
