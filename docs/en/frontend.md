# Frontend State and UI/UX Conventions

English | [简体中文](../frontend.md)

Related: [Documentation Index](./index.md) · [Overview and Architecture](./overview.md) · [API Contract](./api-contract.md)

---

## 1. State Layers

| State Type | Owner | Notes |
|------------|-------|-------|
| Persistent domain data (collections/environments/history) | TanStack Query + Go | Go is the source of truth; Query handles caching and invalidation |
| UI session state (open tabs and active tab) | Zustand | Isolated by workspace; the recoverable subset is stored in WebView `localStorage` |
| In-progress drafts (unsaved request changes) | Zustand (per tab) | Separate from saved state; supports dirty markers, close confirmation, and recovery |
| Real-time streams (progress/WS messages/logs) | Event subscription -> local store | Merge Wails events into the corresponding tab |

`localStorage` is not the Secret Vault. Serialization clears secret parameters for known auth types and clears every parameter for unknown auth types. URLs, ordinary headers, bodies, and scripts remain request content and are not heuristically redacted. Put credentials in the Auth editor or `type=secret` variables instead of hard-coding them in ordinary request fields.

---

## 2. Request-Sending Data Flow

```text
User clicks Send
  -> Build SendContext (current tab draft + active environment + local overrides)
  -> Call IPC sendRequest(...) backed by Wails SendRequest; optimistically mark tab as sending
  -> Subscribe to request:progress to update progress and timing
  -> Receive ResponseResult -> update tab response state + invalidate history query cache
  -> On error -> structured AppError -> inline error panel (no global toast)
```

---

## 3. Typed IPC Wrapper

All invocations of Wails-generated binding functions go through domain-specific wrappers under `frontend/src/ipc/`. Parameters and return values use the [shared types](./data-model.md#3-shared-frontendbackend-types-contract). Components must not call generated Wails bindings directly. This preserves type safety, enables mocked tests, and centralizes error conversion.

---

## 4. UI/UX Interaction Conventions

- **Overall layout**: top workspace/environment switcher -> collection/history sidebar with a draggable width -> central multi-tab editor -> response area below the request with a draggable height.
- **Multiple tabs**: one tab per request; a dot marks unsaved state; tabs can be dragged to reorder and middle-clicked to close.
- **Key-value tables**: Headers, Query, and forms share behavior: an empty final row is added automatically; rows have enable toggles; pasted `k: v` or tabular clipboard content is split into columns; users can switch to `Bulk Edit` text mode.
- **URL ↔ Params bidirectional sync**: when the URL input blurs, Enter is pressed, or a suggestion is picked, the query segment is parsed into the Params table (edits to the base or fragment leave the table untouched); editing Params rows serializes back into the URL immediately. `{{var}}` templates stay raw (no percent-encoding); disabled rows are stripped from the URL because the backend `AddParams` gives URL query precedence — keeping them would make unchecking ineffective. Legacy requests whose params live only in the table keep their rows when the URL gains a query.
- **Variable hints**: typing `{{` opens variable completion. Hovering a defined variable shows its source and value, with secrets masked. Undefined variables receive a red underline.
- **Assertion assistant** (implemented): the Tests pane of the scripts view offers an "insert assertion" dropdown when a response exists — status code / Content-Type / top-level JSON fields (capped at 5) / duration budget are appended to the script for further tuning.
- **Response area**: Pretty/Raw/Preview modes and search highlighting. Render at most 500,000 body characters, inspect blobs through bounded chunks, and stream full content to a native save destination. Never render an HTML blob's preview fragment as a complete document.
- **Response diff**: the "Diff" button in the response area opens a dialog that line-diffs the current response against a picked history entry (search prefilled with the request URL). "Normalize formatting" is on by default (JSON re-serialized with sorted keys and two-space indent so formatting/key-order noise disappears; non-JSON sides fall back to raw text). Bodies beyond the limits (400k chars or the diff line cap) ask the user to use the Body view instead. The diff is display-only and never affects request sending.

### Default Shortcuts

Customize combinations in Settings -> Shortcuts (must include Ctrl/Cmd or use F1-F12; applies immediately, stored in the `hotkeys` setting).

| Action | Win/Linux | macOS |
|--------|-----------|-------|
| Send request | Ctrl+Enter | Cmd+Enter |
| Save | Ctrl+S | Cmd+S |
| New tab | Ctrl+T | Cmd+T |
| Close tab | Ctrl+W | Cmd+W |
| Switch environment | Ctrl+E | Cmd+E |
| Command palette | Ctrl+K | Cmd+K |

- **Themes and accessibility**: light/dark/system; forms and primary actions provide visible focus and keyboard access.
- **Platform consistency**: map shortcuts as shown above. File selection, drag and drop, clipboard access, and the system browser use Wails runtime capabilities rather than private WebView APIs. Include these critical flows in Windows/macOS smoke tests.
- **Empty states and onboarding**: when no tab is active, prompt users to create a request with Ctrl/Cmd+T.
