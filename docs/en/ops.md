# Security, Quality, and Operations

English | [简体中文](../ops.md)

> Cross-cutting concerns: security, error model, testing strategy, packaging, releases, and performance budgets.
> Back to the [Documentation Index](./index.md).

---

## 1. Security Considerations

- **Credential protection**: store secret variables and OAuth tokens in the system keychain by default (Windows Credential Manager / macOS Keychain). If the system backend is unavailable, derive a key with Argon2id and encrypt the fallback vault with AES-GCM. Mask values in the UI by default; history and script logs use the same redaction policy.
- **Classification boundary**: the Vault protects secret parameters in the Auth model, `type=secret` variables, and the sync password. URLs, ordinary headers, bodies, and scripts are user-controlled request content; the application does not infer whether arbitrary strings are credentials. Reference credentials through structured auth fields or secret variables.
- **Script sandbox**: JavaScript cannot access the host filesystem or arbitrary network destinations and has an execution timeout. See [Request Lifecycle](./request-lifecycle.md).
- **Certificate trust**: allow custom CAs and client certificates, but show an explicit risk warning before disabling SSL verification.
- **Treat imported content as untrusted**: prevent injection while parsing imports and never execute imported scripts automatically.

---

## 2. Error Model and User Feedback

The Go side uses one error type and serializes structured errors for the frontend instead of returning raw strings:

```go
// Error categories: Network/Tls/Script/Storage/Import/Validation.
type ErrorKind string

const (
    KindNetwork    ErrorKind = "network" // dns/connect/tls/timeout/...
    KindTls        ErrorKind = "tls"
    KindScript     ErrorKind = "script"
    KindStorage    ErrorKind = "storage"
    KindImport     ErrorKind = "import"
    KindValidation ErrorKind = "validation"
)

// Sentinel error classified with errors.As.
type AppError struct {
    Kind    ErrorKind `json:"kind"`
    Detail  string    `json:"detail"`
    Phase   string    `json:"phase,omitempty"`  // Script phase: pre/test.
    Line    *uint32   `json:"line,omitempty"`   // Script error line.
    Format  string    `json:"format,omitempty"` // Import format on failure.
}

func (e *AppError) Error() string { return string(e.Kind) + ": " + e.Detail }
```

- Categorize **network errors** for display, including DNS failure, connection refused, TLS handshake failure, and timeout, and provide actionable suggestions.
- Include the pre/test phase and line number in **script errors** so the editor can navigate directly to the failure.
- Frontend error boundaries isolate requests: one failed request does not affect other tabs. Global failures use a toast and are retained in logs.

---

## 3. Testing Strategy

| Layer | Coverage | Tools |
|-------|----------|-------|
| Go unit tests | Variable resolution, templates, auth signing, import/export converters, code generation | `go test` |
| Go integration tests | Complete `SendRequest` flow against a mock HTTP server | `go test` + `net/http/httptest` |
| Script engine tests | `pm.*` behavior, assertions, timeouts, and isolation | `go test` |
| Frontend unit tests | Store logic, IPC wrappers, and pure components | Vitest + Testing Library |
| E2E | Critical flow: create request -> send -> inspect response -> save to collection | Wails + Playwright/WebDriver |
| Cross-platform smoke | Windows/macOS builds launch, send one request, and read/write the application-data directory | CI matrix |

Keep core invariants in pure Go functions so they can receive high unit-test coverage without the UI.

### 3.1 Windows/macOS Smoke-Test Gate

For every merge to the main branch and every release candidate, run on native Windows and macOS runners:

1. Build the platform package, install it, and launch the application.
2. Create a workspace and request, then send one HTTP request to a local mock server.
3. Verify SQLite, `blobs/`, and logs are written beneath application-data paths resolved through Wails runtime APIs and Go `path/filepath`.
4. Write and read one secret variable. In CI environments without a system keychain, use master-password encryption fallback.
5. Verify Ctrl/Cmd+Enter and the file picker.
6. Verify signing status; for macOS release artifacts, also verify the notarization ticket.

---

## 4. Cross-Platform Support and Releases

### 4.1 CI Build Matrix and Artifacts

| Platform | GitHub Actions Runner | Architecture | Artifact | Pre-Release Checks |
|----------|-----------------------|--------------|----------|--------------------|
| Windows | `windows-latest` | x64 | `.exe` from `wails build`, optionally NSIS `.msi` | Authenticode signing; ensure WebView2 Runtime is present or guide installation |
| macOS | `macos-latest` | Apple Silicon + Intel, universal or separate | `.app`/`.dmg` from `wails build` | Developer ID signing, Hardened Runtime, notarization, and stapling |
| Linux | `ubuntu-latest` | x64 | `.AppImage` / `.deb` | Best-effort build and startup check |

- **Update path**: Wails has no built-in updater. Each stable release publishes `SHA256SUMS` and `update-manifest.json`; the Settings action currently opens the official release download page. No silent replacement is attempted until signature verification, rollback, and atomic replacement are specified.
- **Signing status**: when Windows/macOS secrets are present, CI performs Authenticode or Developer ID signing plus notarization/stapling. Without them, artifact names include `-unsigned` and a platform-specific `SIGNING_STATUS-*.txt` is published.
- **Crash reporting and telemetry**: optional, disabled by default, and disclosed clearly. Retain rotating local logs for diagnostics.

### 4.2 Platform Implementation Boundaries

- Business modules must not scatter build tags (`//go:build`) or `runtime.GOOS` branches that call OS APIs directly. All platform differences go through the [platform abstraction](./overview.md#4-the-platform-abstraction).
- Resolve application data, cache, logs, database, and the `blobs/` root through Wails runtime path APIs and Go `path/filepath`; never hard-code user directories or concatenate path strings.
- If system proxy, certificate store, or keychain access fails, return a diagnosable structured error and preserve a manual configuration or master-password fallback. Never silently fall back to insecure settings.
- Use consistent Wails/Radix capabilities for WebView differences. Include shortcuts, drag and drop, file dialogs, and clipboard behavior in the two-platform smoke tests.

---

## 5. Non-Functional Targets (Performance Budgets)

| Metric | Target |
|--------|--------|
| Cold start to interactive | < 1.5s |
| Overhead for a simple request, excluding the network | < 20ms |
| Format and render a 1 MB JSON response | < 300ms; virtualize/collapse above the threshold |
| Idle memory | < 200 MB |
| Scroll a history list with 100,000 entries | Stable 60 fps through virtualization |
| Installer size | < 30 MB per platform; prefer CM6 to Monaco, with Go + system WebView far smaller than Electron |
