// Workspace Session store：每个工作区独立保存标签、活动页与可恢复草稿。
import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import type { AppError, HttpRequest, RequestProgress, ResponseResult } from '../ipc';
import { newDefaultRequest } from '../ipc';
import { formatMessage } from '../i18n/locale';

export interface Tab {
  id: string;
  workspaceId: string;
  nodeId?: string;
  name: string;
  draft: HttpRequest;
  revision: number;
  dirty: boolean;
  sending: boolean;
  sendId?: string;
  response?: ResponseResult;
  error?: AppError;
  progress?: RequestProgress;
}

export interface WorkspaceSession {
  tabs: Tab[];
  activeId: string | null;
}

interface TabsState {
  sessions: Record<string, WorkspaceSession>;
  openBlank(workspaceId: string): string;
  openNode(workspaceId: string, nodeId: string, name: string, req: HttpRequest): string;
  close(tabId: string): Tab | undefined;
  reorderTabs(workspaceId: string, fromId: string, toId: string): void;
  removeSession(workspaceId: string): void;
  detachNodes(workspaceId: string, nodeIds: string[]): void;
  setActive(tabId: string): void;
  patchDraft(tabId: string, patch: Partial<HttpRequest>): void;
  markSaved(tabId: string, expectedNodeId: string | undefined, nodeId: string, name: string, savedRevision: number): void;
  setSending(tabId: string, sending: boolean, sendId?: string): void;
  setResponse(tabId: string, sendId: string, response: ResponseResult): boolean;
  setError(tabId: string, sendId: string, error: AppError): boolean;
  setProgress(progress: RequestProgress): void;
}

let seq = 0;
const nextId = () => `tab-${Date.now()}-${seq++}`;
const emptySession = (): WorkspaceSession => ({ tabs: [], activeId: null });

let persistTimer: ReturnType<typeof setTimeout> | null = null;
let pendingPersistKey = '';
let pendingPersistValue = '';

export function flushWorkspaceSessions(): void {
  if (!pendingPersistKey) return;
  if (persistTimer !== null) clearTimeout(persistTimer);
  persistTimer = null;
  const key = pendingPersistKey;
  const value = pendingPersistValue;
  localStorage.setItem(key, value);
  pendingPersistKey = '';
  pendingPersistValue = '';
}

if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', () => {
    try {
      flushWorkspaceSessions();
    } catch {
      // The browser is already unloading; App reports explicit-close failures.
    }
  });
}

function updateTab(
  sessions: Record<string, WorkspaceSession>,
  tabId: string,
  updater: (tab: Tab) => Tab,
): Record<string, WorkspaceSession> {
  for (const [workspaceId, session] of Object.entries(sessions)) {
    const index = session.tabs.findIndex((tab) => tab.id === tabId);
    if (index < 0) continue;
    const tabs = [...session.tabs];
    tabs[index] = updater(tabs[index]);
    return { ...sessions, [workspaceId]: { ...session, tabs } };
  }
  return sessions;
}

export function serializeWorkspaceSessions(sessions: Record<string, WorkspaceSession>) {
  const persisted: Record<string, WorkspaceSession> = {};
  for (const [workspaceId, session] of Object.entries(sessions)) {
    const tabs = session.tabs
      .filter((tab) => tab.dirty)
      .map(({ response: _response, error: _error, sendId: _sendId, progress: _progress, ...tab }) => ({
        ...tab,
        draft: draftWithoutPersistedCredentials(tab.draft),
        sending: false,
      }));
    if (tabs.length === 0) continue;
    persisted[workspaceId] = {
      tabs,
      activeId: tabs.some((tab) => tab.id === session.activeId) ? session.activeId : tabs[0].id,
    };
  }
  return persisted;
}

type PersistedTabsState = { sessions?: Record<string, WorkspaceSession> };

export function migrateWorkspaceSessions(persistedState: unknown): PersistedTabsState {
  const raw = persistedState as PersistedTabsState | undefined;
  if (!raw?.sessions || typeof raw.sessions !== 'object') return { sessions: {} };
  const sessions: Record<string, WorkspaceSession> = {};
  for (const [workspaceId, session] of Object.entries(raw.sessions)) {
    if (!session || !Array.isArray(session.tabs)) continue;
    const tabs = session.tabs
      .filter((tab) => tab?.draft && tab.dirty)
      .map((tab) => ({
        ...tab,
        revision: Number.isFinite(tab.revision) ? tab.revision : 0,
        draft: draftWithoutPersistedCredentials(tab.draft),
        sending: false,
        sendId: undefined,
        response: undefined,
        error: undefined,
        progress: undefined,
      }));
    if (tabs.length === 0) continue;
    sessions[workspaceId] = {
      tabs,
      activeId: tabs.some((tab) => tab.id === session.activeId) ? session.activeId : tabs[0].id,
    };
  }
  return { sessions };
}

const sensitiveAuthParams: Record<string, Set<string>> = {
  basic: new Set(['password']),
  digest: new Set(['password']),
  bearer: new Set(['token']),
  apikey: new Set(['value']),
  oauth1: new Set(['consumersecret', 'token', 'tokensecret']),
  oauth2: new Set(['clientsecret', 'password', 'accesstoken', 'refreshtoken']),
  awsv4: new Set(['secretkey', 'sessiontoken']),
};

const normalizedAuthKey = (key: string) => key.toLowerCase().replace(/[_\-\s]/g, '');

function draftWithoutPersistedCredentials(draft: HttpRequest): HttpRequest {
  const sensitive = sensitiveAuthParams[draft.auth?.type?.toLowerCase() ?? ''];
  const isSensitiveKey = (key: string) =>
    /authorization|cookie|token|secret|api[-_]?key|password|passwd/i.test(key.trim());
  const headers = draft.headers?.map((header) =>
    isSensitiveKey(header.key)
      ? { ...header, value: '' }
      : header,
  );
  const params = draft.params?.map((param) =>
    isSensitiveKey(param.key) ? { ...param, value: '' } : param,
  );
  const body = draft.body
    ? {
        ...draft.body,
        ...(draft.body.items
          ? {
              items: draft.body.items.map((item) =>
                item.type !== 'file' && isSensitiveKey(item.key) ? { ...item, value: '' } : item,
              ),
            }
          : {}),
      }
    : draft.body;
  return {
    ...draft,
    ...(headers ? { headers } : {}),
    ...(params ? { params } : {}),
    ...(body ? { body } : {}),
    ...(draft.auth?.params
      ? {
          auth: {
            ...draft.auth,
            params: Object.fromEntries(
              Object.entries(draft.auth.params).map(([key, value]) => [
                key,
                !sensitive || sensitive.has(normalizedAuthKey(key)) ? '' : value,
              ]),
            ),
          },
        }
      : {}),
  } as HttpRequest;
}

export const useTabs = create<TabsState>()(
  persist(
    (set, get) => ({
      sessions: {},

      openBlank(workspaceId) {
        const tab: Tab = {
          id: nextId(),
          workspaceId,
          name: formatMessage('新请求'),
          draft: newDefaultRequest(),
          revision: 0,
          dirty: false,
          sending: false,
        };
        set((state) => {
          const session = state.sessions[workspaceId] ?? emptySession();
          return {
            sessions: {
              ...state.sessions,
              [workspaceId]: { tabs: [...session.tabs, tab], activeId: tab.id },
            },
          };
        });
        return tab.id;
      },

      openNode(workspaceId, nodeId, name, req) {
        const session = get().sessions[workspaceId] ?? emptySession();
        const existing = session.tabs.find((tab) => tab.nodeId === nodeId);
        if (existing) {
          set((state) => ({
            sessions: {
              ...state.sessions,
              [workspaceId]: { ...session, activeId: existing.id },
            },
          }));
          return existing.id;
        }
        const tab: Tab = {
          id: nextId(),
          workspaceId,
          nodeId,
          name,
          draft: JSON.parse(JSON.stringify(req)) as HttpRequest,
          revision: 0,
          dirty: false,
          sending: false,
        };
        set((state) => ({
          sessions: {
            ...state.sessions,
            [workspaceId]: { tabs: [...session.tabs, tab], activeId: tab.id },
          },
        }));
        return tab.id;
      },

      close(tabId) {
        let removed: Tab | undefined;
        set((state) => {
          for (const [workspaceId, session] of Object.entries(state.sessions)) {
            const index = session.tabs.findIndex((tab) => tab.id === tabId);
            if (index < 0) continue;
            removed = session.tabs[index];
            const tabs = session.tabs.filter((tab) => tab.id !== tabId);
            const activeId =
              session.activeId === tabId
                ? (tabs[Math.min(index, tabs.length - 1)]?.id ?? null)
                : session.activeId;
            return {
              sessions: {
                ...state.sessions,
                [workspaceId]: { tabs, activeId },
              },
            };
          }
          return state;
        });
        return removed;
      },

      reorderTabs(workspaceId, fromId, toId) {
        set((state) => {
          const session = state.sessions[workspaceId];
          if (!session) return state;
          const fromIndex = session.tabs.findIndex((tab) => tab.id === fromId);
          const toIndex = session.tabs.findIndex((tab) => tab.id === toId);
          if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return state;
          const tabs = [...session.tabs];
          const [moved] = tabs.splice(fromIndex, 1);
          tabs.splice(toIndex, 0, moved);
          return {
            sessions: {
              ...state.sessions,
              [workspaceId]: { ...session, tabs },
            },
          };
        });
      },

      removeSession(workspaceId) {
        set((state) => {
          const sessions = { ...state.sessions };
          delete sessions[workspaceId];
          return { sessions };
        });
      },

      detachNodes(workspaceId, nodeIds) {
        if (nodeIds.length === 0) return;
        const removed = new Set(nodeIds);
        set((state) => {
          const session = state.sessions[workspaceId];
          if (!session) return state;
          let changed = false;
          const tabs = session.tabs.map((tab) => {
            if (!tab.nodeId || !removed.has(tab.nodeId)) return tab;
            changed = true;
            return {
              ...tab,
              nodeId: undefined,
              revision: (tab.revision ?? 0) + 1,
              dirty: true,
            };
          });
          if (!changed) return state;
          return {
            sessions: {
              ...state.sessions,
              [workspaceId]: { ...session, tabs },
            },
          };
        });
      },

      setActive(tabId) {
        set((state) => {
          for (const [workspaceId, session] of Object.entries(state.sessions)) {
            if (session.tabs.some((tab) => tab.id === tabId)) {
              return {
                sessions: {
                  ...state.sessions,
                  [workspaceId]: { ...session, activeId: tabId },
                },
              };
            }
          }
          return state;
        });
      },

      patchDraft(tabId, patch) {
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => ({
            ...tab,
            draft: { ...tab.draft, ...patch } as HttpRequest,
            revision: (tab.revision ?? 0) + 1,
            dirty: true,
          })),
        }));
      },

      markSaved(tabId, expectedNodeId, nodeId, name, savedRevision) {
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => {
            if (tab.nodeId !== expectedNodeId) return tab;
            return {
              ...tab,
              nodeId,
              name,
              dirty: (tab.revision ?? 0) !== savedRevision,
            };
          }),
        }));
      },

      setSending(tabId, sending, sendId) {
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => ({
            ...tab,
            sending,
            ...(sending
              ? {
                  sendId,
                  error: undefined,
                  progress: { sendId: sendId ?? '', phase: 'sending', bytesReceived: 0, totalBytes: 0 },
                }
              : { sendId: undefined }),
          })),
        }));
      },

      setResponse(tabId, sendId, response) {
        let accepted = false;
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => {
            if (tab.sendId !== sendId) return tab;
            accepted = true;
            return {
              ...tab,
              response,
              error: undefined,
              sending: false,
              sendId: undefined,
              progress: undefined,
            };
          }),
        }));
        return accepted;
      },

      setError(tabId, sendId, error) {
        let accepted = false;
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => {
            if (tab.sendId !== sendId) return tab;
            accepted = true;
            return {
              ...tab,
              error,
              sending: false,
              sendId: undefined,
              progress: undefined,
            };
          }),
        }));
        return accepted;
      },

      setProgress(progress) {
        set((state) => {
          for (const session of Object.values(state.sessions)) {
            const tab = session.tabs.find((candidate) => candidate.sendId === progress.sendId);
            if (tab) {
              return { sessions: updateTab(state.sessions, tab.id, (current) => ({ ...current, progress })) };
            }
          }
          return state;
        });
      },
    }),
    {
      name: 'apirequest.workspace-sessions.v1',
      // 防抖写入：patchDraft 每次按键都触发 persist，同步 localStorage 写盘会卡顿。
      // 用 400ms 防抖批量写入；beforeunload 时立即 flush 保证不丢数据。
      storage: createJSONStorage(() => {
        return {
          getItem: (key: string) => localStorage.getItem(key),
          setItem: (key: string, value: string) => {
            pendingPersistKey = key;
            pendingPersistValue = value;
            if (persistTimer !== null) clearTimeout(persistTimer);
            persistTimer = setTimeout(() => {
              persistTimer = null;
              try {
                localStorage.setItem(key, value);
                if (pendingPersistKey === key && pendingPersistValue === value) {
                  pendingPersistKey = '';
                  pendingPersistValue = '';
                }
              } catch {
                // Explicit application close retries through flushWorkspaceSessions.
              }
            }, 400);
          },
          removeItem: (key: string) => {
            if (persistTimer !== null) {
              clearTimeout(persistTimer);
              persistTimer = null;
              pendingPersistKey = '';
              pendingPersistValue = '';
            }
            localStorage.removeItem(key);
          },
        };
      }),
      partialize: (state) => ({ sessions: serializeWorkspaceSessions(state.sessions) }) as TabsState,
      migrate: (persistedState) => migrateWorkspaceSessions(persistedState) as TabsState,
      onRehydrateStorage: () => () => {
        try {
          flushWorkspaceSessions();
        } catch {
          // Keep scrubbed memory state; explicit application close retries persistence.
        }
      },
      version: 2,
    },
  ),
);
