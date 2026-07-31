# Frontend State and UI/UX Conventions

English | [简体中文](../frontend.md)

Related: [Documentation Index](./index.md) · [Overview and Architecture](./overview.md) · [API Contract](./api-contract.md)

---

## 1. State Layers

| State Type | Owner | Notes |
|------------|-------|-------|
| Persistent domain data (collections/environments/history) | TanStack Query + Go | Go is the source of truth; Query handles caching and invalidation |
| UI session state (open tabs, panel sizes, active environment) | Zustand | Frontend-only, partially persisted to `setting` |
| In-progress drafts (unsaved request changes) | Zustand (per tab) | Separate from saved state; supports dirty markers and discard |
| Real-time streams (progress/WS messages/logs) | Event subscription -> local store | Merge Wails events into the corresponding tab |

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

- **Overall layout**: top workspace/environment switcher -> collapsible collection/history sidebar -> central multi-tab editor -> response area below or beside the request, switchable between vertical and horizontal layouts.
- **Multiple tabs**: one tab per request or protocol session; a dot marks unsaved state; tabs can be pinned, dragged to reorder, and middle-clicked to close.
- **Key-value tables**: Headers, Query, and forms share behavior: an empty final row is added automatically; rows have enable toggles; pasted `k: v` or tabular clipboard content is split into columns; users can switch to `Bulk Edit` text mode.
- **Variable hints**: typing `{{` opens variable completion. Hovering a defined variable shows its source and value, with secrets masked. Undefined variables receive a red underline.
- **Response area**: Pretty/Raw/Preview modes; collapsible JSON tree, path copy, and search highlighting. Above the large-response threshold, show "Summary only; click to load the full response."

### Default Shortcuts

| Action | Win/Linux | macOS |
|--------|-----------|-------|
| Send request | Ctrl+Enter | Cmd+Enter |
| Save | Ctrl+S | Cmd+S |
| New tab | Ctrl+T | Cmd+T |
| Close tab | Ctrl+W | Cmd+W |
| Command palette | Ctrl+K | Cmd+K |
| Switch environment | Ctrl+E | Cmd+E |
| Focus sidebar search | Ctrl+P | Cmd+P |

- **Command palette** (Ctrl/Cmd+K): global actions and fast navigation to collections/requests, designed for keyboard-first use.
- **Themes and accessibility**: light/dark/system; visible focus, sufficient contrast, and complete keyboard navigation. Radix supplies accessible primitives.
- **Platform consistency**: map shortcuts as shown above. File selection, drag and drop, clipboard access, and the system browser use Wails runtime capabilities rather than private WebView APIs. Include these critical flows in Windows/macOS smoke tests.
- **Empty states and onboarding**: start first-time users with a sample collection and a "Send your first request" path.
