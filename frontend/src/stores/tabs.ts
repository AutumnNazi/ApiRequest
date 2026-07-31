// tabs store：打开的标签页与每页的请求草稿 + 响应态（docs/frontend.md §1）。
// 草稿与已保存态分离，脏标记提示未保存。
import { create } from 'zustand';
import type { HttpRequest, ResponseResult, AppError } from '../ipc';
import { newDefaultRequest } from '../ipc';

export interface Tab {
  id: string; // tab 自身 id（即 sendId 前缀）
  nodeId?: string; // 关联的已保存请求节点；无 = 未保存草稿
  name: string;
  draft: HttpRequest;
  dirty: boolean;
  sending: boolean;
  sendId?: string;
  response?: ResponseResult;
  error?: AppError;
}

interface TabsState {
  tabs: Tab[];
  activeId: string | null;
  openBlank(): void;
  openNode(nodeId: string, name: string, req: HttpRequest): void;
  close(tabId: string): void;
  setActive(tabId: string): void;
  patchDraft(tabId: string, patch: Partial<HttpRequest>): void;
  markSaved(tabId: string, nodeId: string, name: string): void;
  setSending(tabId: string, sending: boolean, sendId?: string): void;
  setResponse(tabId: string, r: ResponseResult): void;
  setError(tabId: string, e: AppError): void;
}

let seq = 0;
const nextId = () => `tab-${Date.now()}-${seq++}`;

export const useTabs = create<TabsState>((set, get) => ({
  tabs: [],
  activeId: null,

  openBlank() {
    // 始终新建标签（Ctrl+T / 标签栏 + 依赖此行为）。
    // StrictMode 双触发防护放在 App.tsx 的 bootstrap useEffect 里
    // （仅当 tabs.length === 0 时才调用），不要在这里做幂等拦截。
    const t: Tab = {
      id: nextId(),
      name: '新请求',
      draft: newDefaultRequest(),
      dirty: false,
      sending: false,
    };
    set((s) => ({ tabs: [...s.tabs, t], activeId: t.id }));
  },

  openNode(nodeId, name, req) {
    const existing = get().tabs.find((t) => t.nodeId === nodeId);
    if (existing) {
      set({ activeId: existing.id });
      return;
    }
    const t: Tab = {
      id: nextId(),
      nodeId,
      name,
      // 深拷贝：草稿独立于集合树缓存
      draft: JSON.parse(JSON.stringify(req)),
      dirty: false,
      sending: false,
    };
    set((s) => ({ tabs: [...s.tabs, t], activeId: t.id }));
  },

  close(tabId) {
    set((s) => {
      const tabs = s.tabs.filter((t) => t.id !== tabId);
      let activeId = s.activeId;
      if (activeId === tabId) {
        const idx = s.tabs.findIndex((t) => t.id === tabId);
        activeId = tabs[Math.min(idx, tabs.length - 1)]?.id ?? null;
      }
      return { tabs, activeId };
    });
  },

  setActive(tabId) {
    set({ activeId: tabId });
  },

  patchDraft(tabId, patch) {
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tabId ? { ...t, draft: { ...t.draft, ...patch } as HttpRequest, dirty: true } : t,
      ),
    }));
  },

  markSaved(tabId, nodeId, name) {
    set((s) => ({
      tabs: s.tabs.map((t) => (t.id === tabId ? { ...t, nodeId, name, dirty: false } : t)),
    }));
  },

  setSending(tabId, sending, sendId) {
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tabId
          ? { ...t, sending, ...(sending ? { sendId, error: undefined } : { sendId: undefined }) }
          : t,
      ),
    }));
  },

  setResponse(tabId, response) {
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tabId ? { ...t, response, error: undefined, sending: false, sendId: undefined } : t,
      ),
    }));
  },

  setError(tabId, error) {
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tabId ? { ...t, error, sending: false, sendId: undefined } : t,
      ),
    }));
  },
}));
