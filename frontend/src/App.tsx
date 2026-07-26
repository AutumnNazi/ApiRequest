// 主应用：三栏布局（侧栏 / 多标签编辑区 / 响应区），发送与保存动作在此编排
import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import Sidebar from './components/Sidebar';
import RequestEditor from './components/RequestEditor';
import ResponseViewer from './components/ResponseViewer';
import EnvSwitcher from './components/EnvSwitcher';
import CookieManager from './components/CookieManager';
import WsPanel from './components/WsPanel';
import SettingsDialog from './components/SettingsDialog';
import WorkspaceSwitcher from './components/WorkspaceSwitcher';
import GrpcPanel from './components/GrpcPanel';
import ThemeDialog from './components/ThemeDialog';
import { useTabs } from './stores/tabs';
import {
  getDefaultWorkspace,
  sendRequest,
  upsertNode,
  listNodes,
  syncNow,
  toAppError,
  type Node,
  type SendContext,
} from './ipc';

export default function App() {
  const qc = useQueryClient();
  const { tabs, activeId, openBlank, close, setActive } = useTabs();
  const setSending = useTabs((s) => s.setSending);
  const setResponse = useTabs((s) => s.setResponse);
  const setError = useTabs((s) => s.setError);
  const markSaved = useTabs((s) => s.markSaved);

  const { data: defaultWorkspace } = useQuery({
    queryKey: ['workspace'],
    queryFn: getDefaultWorkspace,
  });
  // 当前工作区：默认为 GetDefaultWorkspace，切换后覆盖
  const [workspaceOverride, setWorkspaceOverride] = useState<{ id: string; name: string } | null>(
    null,
  );
  const workspace = workspaceOverride ?? defaultWorkspace;
  const [showCookies, setShowCookies] = useState(false);
  const [showWs, setShowWs] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [showGrpc, setShowGrpc] = useState(false);
  const [showTheme, setShowTheme] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [syncMsg, setSyncMsg] = useState('');

  const handleSync = async () => {
    if (!workspace || syncing) return;
    setSyncing(true);
    setSyncMsg('');
    try {
      const r = await syncNow(workspace.id);
      setSyncMsg(
        r.remoteFresh
          ? `已初始化远端（上传 ${r.pushed} 项）`
          : `↑${r.pushed} ↓${r.pulled}${r.deleted ? ` 删${r.deleted}` : ''}`,
      );
      qc.invalidateQueries({ queryKey: ['nodes', workspace.id] });
      qc.invalidateQueries({ queryKey: ['envs', workspace.id] });
      qc.invalidateQueries({ queryKey: ['globals', workspace.id] });
    } catch (e) {
      setSyncMsg('同步失败：' + toAppError(e).detail);
    } finally {
      setSyncing(false);
      setTimeout(() => setSyncMsg(''), 5000);
    }
  };

  // 首次进入自动开一个空标签
  useEffect(() => {
    if (useTabs.getState().tabs.length === 0) openBlank();
  }, [openBlank]);

  const active = tabs.find((t) => t.id === activeId);

  const handleSend = async () => {
    if (!active || !workspace || active.sending) return;
    const sendId = `${active.id}-${Date.now()}`;
    setSending(active.id, true);
    try {
      const res = await sendRequest(sendId, active.draft, {
        workspaceId: workspace.id,
        requestId: active.nodeId ?? '',
      } as SendContext);
      setResponse(active.id, res);
      qc.invalidateQueries({ queryKey: ['history', workspace.id] });
    } catch (e) {
      setError(active.id, toAppError(e));
    }
  };

  const handleSave = async () => {
    if (!active || !workspace) return;
    try {
      // 已关联节点 → 更新；未关联 → 存入第一个集合（无集合则先建默认集合）
      let parentId = '';
      let existing: Partial<Node> = {};
      if (active.nodeId) {
        existing = { id: active.nodeId };
      } else {
        const nodes = await listNodes(workspace.id);
        let col = nodes.find((n) => n.kind === 'collection');
        if (!col) {
          col = await upsertNode({
            workspaceId: workspace.id,
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
        workspaceId: workspace.id,
        ...(parentId ? { parentId } : {}),
        kind: 'request',
        name,
        request: active.draft,
      } as unknown as Node);
      markSaved(active.id, saved.id, saved.name);
      qc.invalidateQueries({ queryKey: ['nodes', workspace.id] });
    } catch (e) {
      setError(active.id, toAppError(e));
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
        openBlank();
      } else if (e.key === 'w') {
        e.preventDefault();
        if (activeId) close(activeId);
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
          className="ml-2 text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1 disabled:opacity-50"
          onClick={handleSync}
          disabled={syncing}
          title="WebDAV 同步（先在 ⚙ 设置里配置）"
        >
          {syncing ? '⇅ 同步中…' : '⇅ 同步'}
        </button>
        {syncMsg && (
          <span
            className={`ml-2 text-xs ${syncMsg.startsWith('同步失败') ? 'text-red-500' : 'text-green-600'}`}
          >
            {syncMsg}
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
      {showCookies && <CookieManager onClose={() => setShowCookies(false)} />}
      {showWs && <WsPanel onClose={() => setShowWs(false)} />}
      {showSettings && <SettingsDialog onClose={() => setShowSettings(false)} />}
      {showGrpc && <GrpcPanel onClose={() => setShowGrpc(false)} />}
      {showTheme && <ThemeDialog onClose={() => setShowTheme(false)} />}

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
                  if (e.button === 1) close(t.id); // 中键关闭
                }}
              >
                <span className="truncate max-w-40">
                  {t.dirty && <span className="text-blue-500 mr-0.5">•</span>}
                  {t.name}
                </span>
                <button
                  className="text-gray-400 hover:text-gray-700 ml-1"
                  onClick={(e) => {
                    e.stopPropagation();
                    close(t.id);
                  }}
                >
                  ×
                </button>
              </div>
            ))}
            <button
              className="px-3 text-gray-400 hover:text-gray-700"
              onClick={openBlank}
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
                  onSave={handleSave}
                />
              </div>
              <div className="h-1/2 min-h-0">
                <ResponseViewer
                  response={active.response}
                  error={active.error}
                  sending={active.sending}
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
