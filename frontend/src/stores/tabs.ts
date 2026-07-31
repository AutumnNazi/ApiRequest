// Workspace Session store：每个工作区独立保存标签、活动页与可恢复草稿。
import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import type { AppError, HttpRequest, RequestProgress, ResponseResult } from '../ipc';
import { newDefaultRequest } from '../ipc';

export interface Tab {
  id: string;
  workspaceId: string;
  nodeId?: string;
  name: string;
  draft: HttpRequest;
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
  close(tabId: string): void;
  removeSession(workspaceId: string): void;
  setActive(tabId: string): void;
  patchDraft(tabId: string, patch: Partial<HttpRequest>): void;
  markSaved(tabId: string, nodeId: string, name: string): void;
  setSending(tabId: string, sending: boolean, sendId?: string): void;
  setResponse(tabId: string, response: ResponseResult): void;
  setError(tabId: string, error: AppError): void;
  setProgress(progress: RequestProgress): void;
}

let seq = 0;
const nextId = () => `tab-${Date.now()}-${seq++}`;
const emptySession = (): WorkspaceSession => ({ tabs: [], activeId: null });

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
  const copy = JSON.parse(JSON.stringify(draft)) as HttpRequest;
  const sensitive = sensitiveAuthParams[copy.auth?.type?.toLowerCase() ?? ''];
  if (copy.auth?.params) {
    copy.auth.params = Object.fromEntries(
      Object.entries(copy.auth.params).map(([key, value]) => [
        key,
        !sensitive || sensitive.has(normalizedAuthKey(key)) ? '' : value,
      ]),
    );
  }
  return copy;
}

export const useTabs = create<TabsState>()(
  persist(
    (set, get) => ({
      sessions: {},

      openBlank(workspaceId) {
        const tab: Tab = {
          id: nextId(),
          workspaceId,
          name: '新请求',
          draft: newDefaultRequest(),
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
        set((state) => {
          for (const [workspaceId, session] of Object.entries(state.sessions)) {
            const index = session.tabs.findIndex((tab) => tab.id === tabId);
            if (index < 0) continue;
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
      },

      removeSession(workspaceId) {
        set((state) => {
          const sessions = { ...state.sessions };
          delete sessions[workspaceId];
          return { sessions };
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
            dirty: true,
          })),
        }));
      },

      markSaved(tabId, nodeId, name) {
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => ({
            ...tab,
            nodeId,
            name,
            dirty: false,
          })),
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

      setResponse(tabId, response) {
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => ({
            ...tab,
            response,
            error: undefined,
            sending: false,
            sendId: undefined,
            progress: undefined,
          })),
        }));
      },

      setError(tabId, error) {
        set((state) => ({
          sessions: updateTab(state.sessions, tabId, (tab) => ({
            ...tab,
            error,
            sending: false,
            sendId: undefined,
            progress: undefined,
          })),
        }));
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
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ sessions: serializeWorkspaceSessions(state.sessions) }) as TabsState,
      version: 1,
    },
  ),
);
