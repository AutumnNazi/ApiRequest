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
  <a href="https://github.com/AutumnNazi/ApiRequest/actions/workflows/dev-build.yml"><img src="https://img.shields.io/github/actions/workflow/status/AutumnNazi/ApiRequest/dev-build.yml?branch=dev&style=flat-square&label=Build" alt="Build" /></a>
</p>

<p align="center">
  <b>Language</b>: English · <a href="README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="#-quick-start"><b>⚡ Quick Start</b></a>
  ·
  <a href="#-key-features"><b>✨ Features</b></a>
  ·
  <a href="docs/en/index.md"><b>📚 Design Docs</b></a>
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

`Go 1.26.5` · `Wails v2` · `React 18` · `TypeScript` · `Vite` · `Tailwind CSS` · `Zustand` · `TanStack Query` · `CodeMirror 6` · `goja` · `modernc.org/sqlite` (pure Go, no CGO)

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

- [Go](https://go.dev/dl/) 1.26.5+
- [Node.js](https://nodejs.org/) 20.19+ on Node 20, or 22.12+
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
go test ./...                  # Go core and Wails bindings
cd frontend && npm test -- --run
cd frontend && npm run build   # type-check + bundle
```

Platform notes: Windows needs the **WebView2 Runtime** (preinstalled on Win 11 / recent Win 10); macOS and Linux use the system WebView (WebKit / WebKitGTK).

### Downloads

[Stable releases](https://github.com/AutumnNazi/ApiRequest/releases/latest) and [`dev-latest`](https://github.com/AutumnNazi/ApiRequest/releases/tag/dev-latest) publish native packages for both supported architectures:

| Platform | Architecture | Packages |
|----------|--------------|----------|
| Windows | AMD64 | `ApiRequest-<version>-Windows-Amd64-Installer.msi`, `-Portable.exe`, and `-Portable.zip` |
| Windows | ARM64 | `ApiRequest-<version>-Windows-Arm64-Installer.msi`, `-Portable.exe`, and `-Portable.zip` |
| macOS | Intel | `ApiRequest-<version>-MacOS-Amd64.dmg` |
| macOS | Apple Silicon | `ApiRequest-<version>-MacOS-Arm64.dmg` |

Use the MSI for a normal Windows installation or a Portable package without installation. Linux AMD64 is published as an additional best-effort executable. Verify downloads against `SHA256SUMS`. Treat development builds and packages not explicitly described as signed in the release notes as unsigned.

---

## 🗺 Status & Roadmap

Core loop (build → send → inspect → save → replay), variables & scripts, all auth types, import/export, Runner, Mock, WS/SSE, gRPC, Git-friendly collection mirror, WebDAV sync, and the headless CLI runner are **implemented and tested**. Recent hardening includes Secret Vault credential storage, workspace-isolated sessions, bounded large-response rendering, resumable SSE, and native file dialogs.

Full checklist and current backlog: [docs/en/roadmap.md](docs/en/roadmap.md) · Architecture decisions: [docs/en/decisions.md](docs/en/decisions.md)

---

## 📚 Documentation

English design docs live in [`docs/en/`](docs/en/) — start at [docs/en/index.md](docs/en/index.md):

- New to the project → [overview](docs/en/overview.md) → [roadmap](docs/en/roadmap.md)
- Hacking on core → [data model](docs/en/data-model.md) → [request lifecycle](docs/en/request-lifecycle.md) → [API contract](docs/en/api-contract.md)

---

## 🤝 Contributing

Issues and PRs welcome. **Do not push directly to `dev`** — fork the repo, branch from `dev` (`fix/*` / `feature/*`), and open a PR against `dev`. PRs are merged via **Squash and merge**.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branch model, PR requirements, and release flow. Please include version, OS, and repro steps in bug reports.

---

## License

To be determined.

<p align="center">
  <sub>Built for people who live in requests, headers, and status codes.</sub>
</p>
