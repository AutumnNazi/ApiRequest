// 主应用：三栏布局（侧栏 / 多标签编辑区 / 响应区），发送与保存动作在此编排
import { lazy, Suspense, useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import Sidebar from './components/Sidebar';
import RequestEditor from './components/RequestEditor';
import ResponseViewer from './components/ResponseViewer';
import EnvSwitcher from './components/EnvSwitcher';
import WorkspaceSwitcher from './components/WorkspaceSwitcher';
import { useDialog } from './components/DialogProvider';
import { formatMessage, useLocale, Verbatim } from './i18n/locale';

// 仅在打开时加载，降低初始渲染的脚本体积。
const CookieManager = lazy(() => import('./components/CookieManager'));
const WsPanel = lazy(() => import('./components/WsPanel'));
const SettingsDialog = lazy(() => import('./components/SettingsDialog'));
const GrpcPanel = lazy(() => import('./components/GrpcPanel'));
const GraphqlPanel = lazy(() => import('./components/GraphqlPanel'));
const ThemeDialog = lazy(() => import('./components/ThemeDialog'));
import { useTabs, type Tab } from './stores/tabs';
import {
  getDefaultWorkspace,
  sendRequest,
  cancelRequest,
  upsertNode,
  listNodes,
  syncNow,
  onRequestProgress,
  toAppError,
  type Node,
  type SendContext,
} from './ipc';

export default function App() {
  const qc = useQueryClient();
  const dialog = useDialog();
  const locale = useLocale((state) => state.locale);
  const sessions = useTabs((s) => s.sessions);
  const openBlank = useTabs((s) => s.openBlank);
  const close = useTabs((s) => s.close);
  const setActive = useTabs((s) => s.setActive);
  const setSending = useTabs((s) => s.setSending);
  const setResponse = useTabs((s) => s.setResponse);
  const setError = useTabs((s) => s.setError);
  const markSaved = useTabs((s) => s.markSaved);
  const setProgress = useTabs((s) => s.setProgress);

  const { data: defaultWorkspace } = useQuery({
    queryKey: ['workspace'],
    queryFn: getDefaultWorkspace,
  });
  // 当前工作区：默认为 GetDefaultWorkspace，切换后覆盖
  const [workspaceOverride, setWorkspaceOverride] = useState<{ id: string; name: string } | null>(
    null,
  );
  const workspace = workspaceOverride ?? defaultWorkspace;
  const session = workspace ? sessions[workspace.id] : undefined;
  const tabs = session?.tabs ?? [];
  const activeId = session?.activeId ?? null;
  const [showCookies, setShowCookies] = useState(false);
  const [showWs, setShowWs] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [showGrpc, setShowGrpc] = useState(false);
  const [showGraphql, setShowGraphql] = useState(false);
  const [showTheme, setShowTheme] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [syncMsg, setSyncMsg] = useState('');
  const [syncFailed, setSyncFailed] = useState(false);

  useEffect(() => onRequestProgress(setProgress), [setProgress]);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const handleSync = async () => {
    if (!workspace || syncing) return;
    setSyncing(true);
    setSyncMsg('');
    setSyncFailed(false);
    try {
      const r = await syncNow(workspace.id);
      setSyncMsg(
        r.remoteFresh
          ? formatMessage('已初始化远端（上传 {count} 项）', { count: r.pushed })
          : formatMessage('↑{pushed} ↓{pulled}{deleted}', {
              pushed: r.pushed,
              pulled: r.pulled,
              deleted: r.deleted ? formatMessage(' 删除 {count}', { count: r.deleted }) : '',
            }),
      );
      qc.invalidateQueries({ queryKey: ['nodes', workspace.id] });
      qc.invalidateQueries({ queryKey: ['envs', workspace.id] });
      qc.invalidateQueries({ queryKey: ['globals', workspace.id] });
    } catch (e) {
      setSyncFailed(true);
      setSyncMsg(formatMessage('同步失败：{detail}', { detail: toAppError(e).detail }));
    } finally {
      setSyncing(false);
      setTimeout(() => setSyncMsg(''), 5000);
    }
  };

  // 每个 Workspace Session 首次进入时各自创建一个空标签；已恢复的草稿保持原样。
  useEffect(() => {
    if (!workspace) return;
    const restored = useTabs.getState().sessions[workspace.id];
    if (!restored || restored.tabs.length === 0) openBlank(workspace.id);
  }, [openBlank, workspace?.id]);

  const active = tabs.find((t) => t.id === activeId);

  const closeTab = async (tab: Tab) => {
    if (
      tab.dirty &&
      !(await dialog.confirm(formatMessage('关闭「{name}」并放弃未保存的修改？', { name: tab.name })))
    ) return;
    close(tab.id);
  };

  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      const hasDirtyDraft = Object.values(useTabs.getState().sessions).some((item) =>
        item.tabs.some((tab) => tab.dirty),
      );
      if (hasDirtyDraft) event.preventDefault();
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, []);

  const handleSend = async () => {
    if (!active || !workspace || active.workspaceId !== workspace.id || active.sending) return;
    // 空 URL 直接前端拦截（与 RequestEditor 发送按钮 disabled 条件一致）
    if (!active.draft.url.trim()) return;
    const tabId = active.id;
    const sendId = `${tabId}-${Date.now()}`;
    setSending(tabId, true, sendId);
    try {
      const res = await sendRequest(sendId, active.draft, {
        workspaceId: active.workspaceId,
        requestId: active.nodeId ?? '',
      } as SendContext);
      setResponse(tabId, res);
      qc.invalidateQueries({ queryKey: ['history', active.workspaceId] });
      // 脚本可能改了环境/全局变量，刷新 EnvSwitcher 缓存
      qc.invalidateQueries({ queryKey: ['envs', active.workspaceId] });
      qc.invalidateQueries({ queryKey: ['globals', active.workspaceId] });
    } catch (e) {
      setError(tabId, toAppError(e));
    }
  };

  const handleCancel = async () => {
    if (!active?.sendId) return;
    try {
      await cancelRequest(active.sendId);
    } catch (e) {
      // 取消失败时仍保留发送状态，等待原请求自然结束。
      void dialog.alert(
        formatMessage('取消请求失败: {detail}', { detail: toAppError(e).detail }),
        { title: '请求取消失败' },
      );
    }
  };

  const handleSave = async () => {
    if (!active || !workspace || active.workspaceId !== workspace.id) return;
    try {
      // 已关联节点 → 更新；未关联 → 存入第一个集合（无集合则先建默认集合）
      let parentId = '';
      let existing: Partial<Node> = {};
      if (active.nodeId) {
        existing = { id: active.nodeId };
      } else {
        const nodes = await listNodes(active.workspaceId);
        let col = nodes.find((n) => n.kind === 'collection');
        if (!col) {
          col = await upsertNode({
            workspaceId: active.workspaceId,
            kind: 'collection',
            name: '默认集合',
          } as Node);
        }
        parentId = col.id;
      }
      const name =
        active.name === '新请求' && active.draft.url
          ? active.draft.url.replace(/^https?:\/\//, '').slice(0, 40)
          : active.name;
      const saved = await upsertNode({
        ...existing,
        workspaceId: active.workspaceId,
        ...(parentId ? { parentId } : {}),
        kind: 'request',
        name,
        request: active.draft,
      } as unknown as Node);
      markSaved(active.id, saved.id, saved.name);
      qc.invalidateQueries({ queryKey: ['nodes', active.workspaceId] });
    } catch (e) {
      // 保存失败不应污染响应面板（与发送错误语义不同），单独提示。
      void dialog.alert(formatMessage('保存失败: {detail}', { detail: toAppError(e).detail }), {
        title: '保存失败',
      });
    }
  };

  // 快捷键：Ctrl/Cmd+Enter 发送、Ctrl/Cmd+S 保存、Ctrl/Cmd+T 新标签、Ctrl/Cmd+W 关标签
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;
      if (e.key === 'Enter') {
        e.preventDefault();
        handleSend();
      } else if (e.key === 's') {
        e.preventDefault();
        handleSave();
      } else if (e.key === 't') {
        e.preventDefault();
        if (workspace) openBlank(workspace.id);
      } else if (e.key === 'w') {
        e.preventDefault();
        if (active) void closeTab(active);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  if (!workspace) {
    return (
      <div className="h-screen flex items-center justify-center text-gray-400">初始化中…</div>
    );
  }

  return (
    <div className="h-screen flex flex-col">
      {/* 顶栏 */}
      <header className="flex items-center px-4 py-2 border-b bg-white">
        <h1 className="font-semibold text-sm">ApiRequest</h1>
        <WorkspaceSwitcher
          activeId={workspace.id}
          onSwitch={(id) => setWorkspaceOverride({ id, name: '' })}
        />
        <button
          className="ml-4 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
          onClick={() => setShowCookies(true)}
        >
          Cookies
        </button>
        <button
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
          onClick={() => setShowWs(true)}
          title="WebSocket / SSE 会话"
        >
          WS/SSE
        </button>
        <button
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
          onClick={() => setShowGrpc(true)}
          title="gRPC 反射调用"
        >
          gRPC
        </button>
        <button
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
          onClick={() => setShowGraphql(true)}
          title="GraphQL schema 内省与补全"
        >
          GraphQL
        </button>
        <button
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1 disabled:opacity-50"
          onClick={handleSync}
          disabled={syncing}
          title="WebDAV 同步（先在 ⚙ 设置里配置）"
        >
          {syncing ? '⇅ 同步中…' : '⇅ 同步'}
        </button>
        {syncMsg && (
          <span
            className={`ml-2 text-xs ${syncFailed ? 'text-red-500' : 'text-green-600'}`}
          >
            <Verbatim value={syncMsg} />
          </span>
        )}
        <button
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
          onClick={() => setShowTheme(true)}
          title="主题"
        >
          🎨
        </button>
        <button
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
          onClick={() => setShowSettings(true)}
          title="应用设置"
        >
          ⚙
        </button>
        <EnvSwitcher workspaceId={workspace.id} />
      </header>
      <Suspense fallback={null}>
        {showCookies && <CookieManager onClose={() => setShowCookies(false)} />}
        {showWs && <WsPanel onClose={() => setShowWs(false)} />}
        {showSettings && <SettingsDialog onClose={() => setShowSettings(false)} />}
        {showGrpc && <GrpcPanel onClose={() => setShowGrpc(false)} />}
        {showGraphql && <GraphqlPanel onClose={() => setShowGraphql(false)} />}
        {showTheme && <ThemeDialog onClose={() => setShowTheme(false)} />}
      </Suspense>

      <div className="flex-1 flex min-h-0">
        {/* 侧栏 */}
        <aside className="w-64 shrink-0">
          <Sidebar workspaceId={workspace.id} />
        </aside>

        {/* 编辑 + 响应 */}
        <main className="flex-1 flex flex-col min-w-0">
          {/* 标签栏 */}
          <div className="flex border-b bg-gray-50 text-sm overflow-x-auto">
            {tabs.map((t) => (
              <div
                key={t.id}
                className={`flex items-center gap-1 px-3 py-2 border-r cursor-pointer whitespace-nowrap ${
                  t.id === activeId ? 'bg-white font-medium' : 'text-gray-500 hover:bg-gray-100'
                }`}
                onClick={() => setActive(t.id)}
                onAuxClick={(e) => {
                  if (e.button === 1) closeTab(t); // 中键关闭
                }}
              >
                <span className="truncate max-w-40">
                  {t.dirty && <span className="text-blue-500 mr-0.5">•</span>}
                  <Verbatim value={t.name} />
                </span>
                <button
                  className="text-gray-400 hover:text-gray-700 ml-1"
                  onClick={(e) => {
                    e.stopPropagation();
                    closeTab(t);
                  }}
                >
                  ×
                </button>
              </div>
            ))}
            <button
              className="px-3 text-gray-400 hover:text-gray-700"
              onClick={() => openBlank(workspace.id)}
              title="新建标签 (Ctrl+T)"
            >
              +
            </button>
          </div>

          {active ? (
            <div className="flex-1 flex flex-col min-h-0">
              <div className="h-1/2 min-h-0 border-b">
                <RequestEditor
                  tab={active}
                  workspaceId={workspace.id}
                  onSend={handleSend}
                  onCancel={handleCancel}
                  onSave={handleSave}
                />
              </div>
              <div className="h-1/2 min-h-0">
                <ResponseViewer
                  response={active.response}
                  error={active.error}
                  sending={active.sending}
                  progress={active.progress}
                  nodeId={active.nodeId}
                />
              </div>
            </div>
          ) : (
            <div className="flex-1 flex items-center justify-center text-gray-400 text-sm">
              按 Ctrl+T 新建请求标签
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
