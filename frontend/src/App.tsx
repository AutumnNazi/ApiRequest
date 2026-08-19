// 主应用：三栏布局（侧栏 / 多标签编辑区 / 响应区），发送与保存动作在此编排
import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import Sidebar from './components/Sidebar';
import RequestEditor from './components/RequestEditor';
import ResponseViewer from './components/ResponseViewer';
import Splitter from './components/Splitter';
import WindowControls from './components/WindowControls';
import QueryErrorState from './components/QueryErrorState';
import EnvSwitcher from './components/EnvSwitcher';
import WorkspaceSwitcher from './components/WorkspaceSwitcher';
import { useDialog } from './components/DialogProvider';
import { usePersistentState } from './hooks/usePersistentState';
import { dragRegion, noDragRegion } from './titlebar';
import { WindowMaximise, WindowUnmaximise, WindowIsMaximised } from '../wailsjs/runtime/runtime';
import { formatMessage, useLocale, Verbatim } from './i18n/locale';
import { collectVarRefs } from './utils/varRefs';
import { ensureHttpScheme, splitRequestAuthHeader } from './utils/request';
import { closeTabSafely, closeTabsSequentially } from './utils/tabClose';
import { isHotkeySuppressed } from './utils/hotkeys';

// 仅在打开时加载，降低初始渲染的脚本体积。
const CookieManager = lazy(() => import('./components/CookieManager'));
const WsPanel = lazy(() => import('./components/WsPanel'));
const SettingsDialog = lazy(() => import('./components/SettingsDialog'));
const GrpcPanel = lazy(() => import('./components/GrpcPanel'));
const GraphqlPanel = lazy(() => import('./components/GraphqlPanel'));
const ThemeDialog = lazy(() => import('./components/ThemeDialog'));
import { flushWorkspaceSessions, useTabs, type Tab } from './stores/tabs';
import { useRequestProgress } from './stores/requestProgress';
import {
  getDefaultWorkspace,
  renameWorkspace,
  sendRequest,
  cancelRequest,
  releaseResponseBlob,
  upsertNode,
  getNode,
  syncNow,
  getSyncConfig,
  onRequestProgress,
  onApplicationCloseRequest,
  requestApplicationQuit,
  toAppError,
  type Node,
  type SendContext,
  type Environment,
  type Variable,
  type Body,
} from './ipc';

function findTab(tabId: string): Tab | undefined {
  for (const session of Object.values(useTabs.getState().sessions)) {
    const tab = session.tabs.find((candidate) => candidate.id === tabId);
    if (tab) return tab;
  }
  return undefined;
}

function ActiveResponse({ tab }: { tab: Tab }) {
  const progress = useRequestProgress((state) =>
    tab.sendId ? state.bySendId[tab.sendId] : undefined,
  );
  return (
    <ResponseViewer
      response={tab.response}
      error={tab.error}
      sending={tab.sending}
      progress={progress}
      nodeId={tab.nodeId}
    />
  );
}

export default function App() {
  const qc = useQueryClient();
  const dialog = useDialog();
  const locale = useLocale((state) => state.locale);
  const sessions = useTabs((s) => s.sessions);
  const openBlank = useTabs((s) => s.openBlank);
  const closeIfUnchanged = useTabs((s) => s.closeIfUnchanged);
  const reorderTabs = useTabs((s) => s.reorderTabs);
  const setActive = useTabs((s) => s.setActive);
  const setSending = useTabs((s) => s.setSending);
  const setResponse = useTabs((s) => s.setResponse);
  const setError = useTabs((s) => s.setError);
  const markSaved = useTabs((s) => s.markSaved);
  const updateProgress = useRequestProgress((state) => state.update);

  const workspaceQuery = useQuery({
    queryKey: ['workspace'],
    queryFn: getDefaultWorkspace,
  });
  const defaultWorkspace = workspaceQuery.data;
  // 首次加载：若默认工作区仍为英文名"My Workspace"，重命名为本地化名称
  useEffect(() => {
    if (defaultWorkspace?.name === 'My Workspace') {
      const localName = formatMessage('我的工作区');
      if (localName !== 'My Workspace') {
        void renameWorkspace(defaultWorkspace.id, localName)
          .then(() => {
            qc.invalidateQueries({ queryKey: ['workspace'] });
            qc.invalidateQueries({ queryKey: ['workspaces'] });
          })
          .catch((cause) => {
            dialog.toast(
              formatMessage('初始化工作区名称失败：{detail}', {
                detail: toAppError(cause).detail,
              }),
              'error',
            );
          });
      }
    }
  }, [defaultWorkspace, dialog, locale, qc]);
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
  // Tab 右键菜单：{x, y, tabId} 定位与目标；null 表示菜单关闭
  const [tabCtx, setTabCtx] = useState<{ x: number; y: number; tabId: string } | null>(null);
  // Tab 拖拽排序：记录被拖拽的源 tab id
  const [dragTabId, setDragTabId] = useState<string | null>(null);
  const closingRef = useRef(false);
  const sendingTabsRef = useRef<Set<string>>(new Set());
  const savingTabsRef = useRef<Set<string>>(new Set());
  // WebDAV 同步配置：仅已配置时展示同步按钮
  const { data: syncCfg } = useQuery({
    queryKey: ['syncConfig'],
    queryFn: getSyncConfig,
  });
  const syncEnabled = !!(syncCfg?.url && syncCfg?.username);
  // 布局持久化：侧栏宽度（px）与编辑区高度比例（0~1）
  const [sidebarWidth, setSidebarWidth] = usePersistentState('apirequest-layout-sidebar', 256);
  const [editorRatio, setEditorRatio] = usePersistentState('apirequest-layout-editor', 0.5);
  // Ctrl/Cmd+E 展开环境下拉：每次按下递增，EnvSwitcher 侧按信号变化响应
  const [envOpenSignal, setEnvOpenSignal] = useState(0);

  useEffect(() => onRequestProgress(updateProgress), [updateProgress]);

  const requestClose = async () => {
    if (closingRef.current) return;
    closingRef.current = true;
    try {
      const hasDirtyDraft = Object.values(useTabs.getState().sessions).some((item) =>
        item.tabs.some((tab) => tab.dirty),
      );
      if (
        hasDirtyDraft
        && !(await dialog.confirm(formatMessage('仍有未保存的请求草稿，确认退出应用？')))
      ) return;
      flushWorkspaceSessions();
      await requestApplicationQuit();
    } catch (cause) {
      void dialog.alert(
        formatMessage('退出应用失败: {detail}', { detail: toAppError(cause).detail }),
        { title: '退出失败' },
      );
    } finally {
      closingRef.current = false;
    }
  };

  const closeRef = useRef(requestClose);
  closeRef.current = requestClose;
  useEffect(() => onApplicationCloseRequest(() => { void closeRef.current(); }), []);

  // 标题栏双击最大化/还原。
  // 非最大化：--wails-draggable:drag 让 Wails 拦截 mousedown 启动原生拖拽，
  //   浏览器无法合成 dblclick，用 mousedown 手动检测双击 → WindowMaximise。
  // 最大化：移除 --wails-draggable，避免 Wails 拦截 mousedown 触发 Windows 原生
  //   “还原并拖拽”行为（第一次按下就会还原窗口，导致双击恢复失效）。
  //   改用原生 dblclick → WindowUnmaximise。
  const [maximised, setMaximised] = useState(false);
  useEffect(() => {
    let alive = true;
    const refresh = () => {
      void WindowIsMaximised()
        .then((m) => alive && setMaximised(m))
        .catch(() => {});
    };
    refresh();
    window.addEventListener('resize', refresh);
    return () => {
      alive = false;
      window.removeEventListener('resize', refresh);
    };
  }, []);

  const lastTitleClick = useRef(0);
  const onTitleMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement).closest('[data-no-drag]')) return;
    if (maximised) return; // 最大化时由 onDoubleClick 处理
    const now = Date.now();
    if (now - lastTitleClick.current < 400) {
      setTimeout(() => WindowMaximise(), 0);
      lastTitleClick.current = 0;
    } else {
      lastTitleClick.current = now;
    }
  };

  const onTitleDoubleClick = () => {
    if (maximised) WindowUnmaximise();
  };

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

  const closeTab = async (tab: Tab): Promise<boolean> => {
    let result: Awaited<ReturnType<typeof closeTabSafely<Tab>>>;
    try {
      result = await closeTabSafely(tab.id, {
        read: findTab,
        confirmDiscard: (current) =>
          dialog.confirm(
            formatMessage('关闭「{name}」并放弃未保存的修改？', { name: current.name }),
          ),
        cancel: cancelRequest,
        commit: (current) =>
          closeIfUnchanged(current.id, current.revision, current.sendId),
      });
    } catch (cause) {
      void dialog.alert(
        formatMessage('关闭标签前取消请求失败: {detail}', { detail: toAppError(cause).detail }),
        { title: '请求取消失败' },
      );
      return false;
    }
    if (!result.continue) return false;
    const removed = result.closed;
    if (!removed) return true;
    const blobRef = removed.response?.body?.blobRef;
    if (blobRef) {
      try {
        await releaseResponseBlob(blobRef);
      } catch (cause) {
        void dialog.alert(
          formatMessage('释放响应文件失败: {detail}', { detail: toAppError(cause).detail }),
          { title: '响应清理失败' },
        );
      }
    }
    return true;
  };

  // 批量关闭：统一用 closeTab 逐个处理（包含 dirty 检查与资源释放）
  const closeOthers = (keepTab: Tab) =>
    closeTabsSequentially(tabs.filter((tab) => tab.id !== keepTab.id), closeTab);
  const closeRight = (anchorTab: Tab) => {
    const idx = tabs.findIndex((t) => t.id === anchorTab.id);
    if (idx < 0) return Promise.resolve(true);
    return closeTabsSequentially(tabs.slice(idx + 1), closeTab);
  };
  const closeLeft = (anchorTab: Tab) => {
    const idx = tabs.findIndex((t) => t.id === anchorTab.id);
    if (idx <= 0) return Promise.resolve(true);
    return closeTabsSequentially(tabs.slice(0, idx), closeTab);
  };
  const closeAll = () => closeTabsSequentially(tabs, closeTab);

  // 点击页面其他位置关闭 tab 右键菜单
  const tabCtxRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!tabCtx) return;
    const onDown = (e: MouseEvent) => {
      if (tabCtxRef.current && !tabCtxRef.current.contains(e.target as HTMLElement)) {
        setTabCtx(null);
      }
    };
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setTabCtx(null);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onEsc);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onEsc);
    };
  }, [tabCtx]);

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
    if (sendingTabsRef.current.has(tabId)) return;
    sendingTabsRef.current.add(tabId);
    try {
      // URL 无协议前缀时自动补充 http://
      const url = ensureHttpScheme(active.draft.url);
      if (url !== active.draft.url) useTabs.getState().patchDraft(tabId, { url });
      const draft = { ...active.draft, url } as typeof active.draft;
      // 发送前变量检查：请求引用的 {{var}} 若既不在激活环境也不在全局变量中，
      // 提醒用户确认（集合级变量与脚本运行时 set 的变量无法在此静态获知）。
      const undefinedVars = findUndefinedVars(qc, active.workspaceId, draft);
      if (undefinedVars.length > 0) {
        const list = undefinedVars.map((n) => `{{${n}}}`).join(', ');
        const ok = await dialog.confirm(
          formatMessage('以下变量未在激活环境或全局变量中定义：{vars}\n\n若由集合级变量或发送前脚本提供，可忽略并继续发送。', {
            vars: list,
          }),
          { title: formatMessage('未定义变量') },
        );
        if (!ok) return;
      }
      const current = findTab(tabId);
      if (!current || current.sendId || current.workspaceId !== active.workspaceId) return;
      const sendId = `${tabId}-${Date.now()}`;
      setSending(tabId, true, sendId);
      useRequestProgress.getState().start(sendId);
      const previousBlobRef = active.response?.body?.blobRef;
      try {
        const res = await sendRequest(sendId, draft, {
          workspaceId: active.workspaceId,
          requestId: active.nodeId ?? '',
        } as SendContext);
        const accepted = setResponse(tabId, sendId, res);
        const responseBlobRef = res.body?.blobRef;
        if (!accepted && responseBlobRef) {
          try {
            await releaseResponseBlob(responseBlobRef);
          } catch {
            // Backend shutdown and workspace cleanup remain the final owner fallback.
          }
        }
        if (accepted && previousBlobRef && previousBlobRef !== responseBlobRef) {
          try {
            await releaseResponseBlob(previousBlobRef);
          } catch {
            // Backend shutdown and workspace cleanup remain the final owner fallback.
          }
        }
        qc.invalidateQueries({ queryKey: ['history', active.workspaceId] });
        qc.invalidateQueries({ queryKey: ['cookies'] });
        // 脚本可能改了环境/全局变量，刷新 EnvSwitcher 缓存
        qc.invalidateQueries({ queryKey: ['envs', active.workspaceId] });
        qc.invalidateQueries({ queryKey: ['globals', active.workspaceId] });
      } catch (e) {
        setError(tabId, sendId, toAppError(e));
      } finally {
        useRequestProgress.getState().clear(sendId);
      }
    } finally {
      sendingTabsRef.current.delete(tabId);
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

  // GraphQL 内省 → 新建请求标签：填入 endpoint + Query/Variables，method=POST
  const handleOpenGraphqlRequest = (req: { url: string; query: string; variables: string; authHeader?: { key: string; value: string } }) => {
    if (!workspace) return;
    const credentials = splitRequestAuthHeader(req.authHeader);
    const tabId = openBlank(workspace.id);
    useTabs.getState().patchDraft(tabId, {
      url: req.url,
      method: 'POST',
      headers: [
        { key: 'Content-Type', value: 'application/json', enabled: true, description: '' },
        ...(credentials.header
          ? [{ ...credentials.header, enabled: true, description: '' }]
          : []),
      ],
      ...(credentials.auth ? { auth: credentials.auth } : {}),
      body: {
        kind: 'graphql',
        query: req.query,
        variables: req.variables,
        text: '',
      } as Body,
    });
  };

  const handleSave = async () => {
    if (!active || !workspace || active.workspaceId !== workspace.id) return;
    const tabId = active.id;
    if (savingTabsRef.current.has(tabId)) return;
    savingTabsRef.current.add(tabId);
    const savedRevision = active.revision ?? 0;
    try {
      // 已关联节点 → 更新；未关联 → 保存到根级（无需先建集合）
      let parentId = '';
      let existing: Partial<Node> = {};
      if (active.nodeId) {
        // 详情按需加载，既保留 parentId，也避免保存请求时清空节点级 auth/脚本/变量。
        const currentNode = await getNode(active.workspaceId, active.nodeId);
        existing = currentNode;
        parentId = currentNode.parentId ?? '';
      }
      const name =
        active.name === formatMessage('新请求') && active.draft.url
          ? active.draft.url.replace(/^https?:\/\//, '').slice(0, 40)
          : active.nodeId
            ? (existing.name ?? active.name)
            : active.name;
      const saved = await upsertNode({
        ...existing,
        workspaceId: active.workspaceId,
        ...(parentId ? { parentId } : {}),
        kind: 'request',
        name,
        request: active.draft,
      } as unknown as Node);
      markSaved(tabId, active.nodeId, saved.id, saved.name, savedRevision);
      qc.invalidateQueries({ queryKey: ['nodes', active.workspaceId] });
    } catch (e) {
      // 保存失败不应污染响应面板（与发送错误语义不同），单独提示。
      void dialog.alert(formatMessage('保存失败: {detail}', { detail: toAppError(e).detail }), {
        title: '保存失败',
      });
    } finally {
      savingTabsRef.current.delete(tabId);
    }
  };

  // 快捷键：Ctrl/Cmd+Enter 发送、Ctrl/Cmd+S 保存、Ctrl/Cmd+T 新标签、Ctrl/Cmd+W 关标签、Ctrl/Cmd+E 切环境
  // 用 ref 持有最新 handler，避免每次渲染都重新绑定 keydown（输入 URL 时频繁重渲染）
  const hotkeyRef = useRef({ handleSend, handleSave, openBlank, closeTab, workspace, active });
  hotkeyRef.current = { handleSend, handleSave, openBlank, closeTab, workspace, active };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;
      if (isHotkeySuppressed(e.target)) return;
      const { handleSend, handleSave, openBlank, closeTab, workspace, active } = hotkeyRef.current;
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
      } else if (e.key === 'e') {
        e.preventDefault();
        setEnvOpenSignal((n) => n + 1);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  if (!workspace && workspaceQuery.isError) {
    return (
      <QueryErrorState
        message={formatMessage('初始化失败')}
        detail={toAppError(workspaceQuery.error).detail}
        onRetry={() => void workspaceQuery.refetch()}
        fullScreen
      />
    );
  }

  if (!workspace) {
    return (
      <div className="h-screen flex items-center justify-center text-gray-400">{formatMessage('初始化中…')}</div>
    );
  }

  return (
    <div className="h-screen flex flex-col">
      {/* 自绘标题栏（无边框窗口）：标题区可拖动，交互控件与窗口按钮 no-drag */}
      <header
        className="flex items-center h-10 px-3 border-b bg-white shrink-0"
        style={maximised ? undefined : dragRegion}
        onMouseDown={onTitleMouseDown}
        onDoubleClick={onTitleDoubleClick}
      >
        <h1 className="hidden px-1 text-sm font-semibold sm:block cursor-default">
          ApiRequest
        </h1>
        {/* 左侧交互控件组（工作区/环境/协议/同步） */}
        <div className="flex items-center min-w-0" data-no-drag style={noDragRegion}>
          {/* 分组1：工作区 + 环境 */}
          <WorkspaceSwitcher
            activeId={workspace.id}
            onSwitch={(id) => setWorkspaceOverride({ id, name: '' })}
          />
          <EnvSwitcher workspaceId={workspace.id} openSignal={envOpenSignal} />

          {/* 分组2：协议工具 */}
          <div className="ml-3 hidden items-center gap-1.5 border-l pl-3 lg:flex">
            <button
              className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
              onClick={() => setShowCookies(true)}
            >
              Cookies
            </button>
            <button
              className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
              onClick={() => setShowWs(true)}
              title={formatMessage('WebSocket / SSE 会话')}
            >
              WS/SSE
            </button>
            <button
              className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
              onClick={() => setShowGrpc(true)}
              title={formatMessage('gRPC 反射调用')}
            >
              gRPC
            </button>
            <button
              className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
              onClick={() => setShowGraphql(true)}
              title={formatMessage('GraphQL schema 内省与补全')}
            >
              GraphQL
            </button>
          </div>

          {/* 分组3：同步（仅 WebDAV 已配置时展示） */}
          {syncEnabled && (
          <div className="ml-3 hidden items-center gap-1.5 border-l pl-3 lg:flex">
            <button
              className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1 disabled:opacity-50"
              onClick={handleSync}
              disabled={syncing}
              title={formatMessage('WebDAV 同步（先在 ⚙ 设置里配置）')}
            >
              {syncing ? formatMessage('⇅ 同步中…') : formatMessage('⇅ 同步')}
            </button>
            {syncMsg && (
              <span
                className={`text-xs ${syncFailed ? 'text-red-500' : 'text-green-600'}`}
              >
                <Verbatim value={syncMsg} />
              </span>
            )}
          </div>
          )}
        </div>
        {/* 中间空白区域：继承 header 的 dragRegion，可拖动窗口；双击最大化/还原 */}
        <div className="flex-1" />
        {/* 右侧交互控件组（主题/设置） */}
        <div className="flex items-center gap-1.5 border-l pl-3" data-no-drag style={noDragRegion}>
          <button
            className="hidden text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1 md:block"
            onClick={() => setShowTheme(true)}
            title={formatMessage('主题')}
          >
            {formatMessage('主题')}
          </button>
          <button
            className="hidden text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1 md:block"
            onClick={() => setShowSettings(true)}
            title={formatMessage('应用设置')}
          >
            {formatMessage('设置')}
          </button>
        </div>
          <WindowControls onClose={() => void requestClose()} />
      </header>
      <Suspense fallback={null}>
        {showCookies && <CookieManager onClose={() => setShowCookies(false)} />}
        {showWs && <WsPanel onClose={() => setShowWs(false)} />}
        {showSettings && <SettingsDialog onClose={() => setShowSettings(false)} />}
        {showGrpc && <GrpcPanel onClose={() => setShowGrpc(false)} />}
        {showGraphql && (
          <GraphqlPanel
            onClose={() => setShowGraphql(false)}
            onOpenRequest={handleOpenGraphqlRequest}
          />
        )}
        {showTheme && <ThemeDialog onClose={() => setShowTheme(false)} />}
      </Suspense>

      {/* Tab 右键菜单：固定定位浮层，点击菜单项后关闭 */}
      {tabCtx && (() => {
        const idx = tabs.findIndex((t) => t.id === tabCtx.tabId);
        const disLeft = idx <= 0 || tabs.length <= 1;
        const disRight = idx < 0 || idx >= tabs.length - 1 || tabs.length <= 1;
        const disOthers = tabs.length <= 1;
        return (
        <div
          ref={tabCtxRef}
          className="fixed z-50 min-w-[160px] rounded border border-gray-200 bg-white py-1 text-sm shadow-lg"
          style={{
            left: Math.min(tabCtx.x, window.innerWidth - 180),
            top: Math.min(tabCtx.y, window.innerHeight - 200),
          }}
        >
          <button
            className="block w-full px-3 py-1.5 text-left hover:bg-gray-100"
            onClick={() => {
              const target = tabs.find((t) => t.id === tabCtx.tabId);
              setTabCtx(null);
              if (target) void closeTab(target);
            }}
          >
            {formatMessage('关闭')}
          </button>
          <button
            className={`block w-full px-3 py-1.5 text-left ${disOthers ? 'text-gray-300 cursor-not-allowed' : 'hover:bg-gray-100'}`}
            disabled={disOthers}
            onClick={() => {
              if (disOthers) return;
              const target = tabs.find((t) => t.id === tabCtx.tabId);
              setTabCtx(null);
              if (target) void closeOthers(target);
            }}
          >
            {formatMessage('关闭其他')}
          </button>
          <button
            className={`block w-full px-3 py-1.5 text-left ${disLeft ? 'text-gray-300 cursor-not-allowed' : 'hover:bg-gray-100'}`}
            disabled={disLeft}
            onClick={() => {
              if (disLeft) return;
              const target = tabs.find((t) => t.id === tabCtx.tabId);
              setTabCtx(null);
              if (target) void closeLeft(target);
            }}
          >
            {formatMessage('关闭左侧')}
          </button>
          <button
            className={`block w-full px-3 py-1.5 text-left ${disRight ? 'text-gray-300 cursor-not-allowed' : 'hover:bg-gray-100'}`}
            disabled={disRight}
            onClick={() => {
              if (disRight) return;
              const target = tabs.find((t) => t.id === tabCtx.tabId);
              setTabCtx(null);
              if (target) void closeRight(target);
            }}
          >
            {formatMessage('关闭右侧')}
          </button>
          <div className="my-1 border-t border-gray-100" />
          <button
            className="block w-full px-3 py-1.5 text-left text-red-600 hover:bg-red-50"
            onClick={() => {
              setTabCtx(null);
              void closeAll();
            }}
          >
            {formatMessage('关闭全部')}
          </button>
        </div>
        );
      })()}

      <div className="flex-1 flex min-h-0">
        {/* 侧栏（可拖拽调整宽度） */}
        <aside className="shrink-0" style={{ width: sidebarWidth }}>
          <Sidebar workspaceId={workspace.id} />
        </aside>
        <Splitter
          orientation="vertical"
          ratio={sidebarWidth / Math.max(window.innerWidth, 1)}
          onRatio={(r) => setSidebarWidth(r * window.innerWidth)}
        />

        {/* 编辑 + 响应 */}
        <main className="flex-1 flex flex-col min-w-0">
          {/* 标签栏 */}
          <div className="flex items-center gap-1 border-b bg-gray-50 px-2 py-1 text-sm overflow-x-auto">
            {tabs.map((t) => (
              <div
                key={t.id}
                draggable
                onDragStart={(e) => {
                  setDragTabId(t.id);
                  e.dataTransfer.effectAllowed = 'move';
                }}
                onDragOver={(e) => {
                  e.preventDefault();
                  e.dataTransfer.dropEffect = 'move';
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  const fromId = dragTabId;
                  setDragTabId(null);
                  if (!fromId || fromId === t.id) return;
                  reorderTabs(workspace.id, fromId, t.id);
                }}
                onDragEnd={() => setDragTabId(null)}
                onContextMenu={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setTabCtx({ x: e.clientX, y: e.clientY, tabId: t.id });
                }}
                className={`flex items-center gap-1 px-3 py-1.5 rounded cursor-pointer whitespace-nowrap ${
                  t.id === activeId ? 'bg-white font-medium shadow-sm' : 'text-gray-500 hover:bg-gray-100'
                } ${dragTabId === t.id ? 'opacity-50' : ''}`}
                onClick={() => setActive(t.id)}
                onAuxClick={(e) => {
                  if (e.button === 1) void closeTab(t); // 中键关闭
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
                    void closeTab(t);
                  }}
                >
                  ×
                </button>
              </div>
            ))}
            <button
              className="border rounded px-2.5 py-0.5 ml-1 text-gray-400 hover:text-gray-700 hover:bg-gray-100 text-sm"
              onClick={() => openBlank(workspace.id)}
              title={formatMessage('新建标签 (Ctrl+T)')}
            >
              +
            </button>
          </div>

          {active ? (
            <div
              className="flex-1 flex flex-col min-h-0"
              style={{ height: '100%' }}
            >
              <div className="min-h-0" style={{ height: `${editorRatio * 100}%` }}>
                <RequestEditor
                  tab={active}
                  workspaceId={workspace.id}
                  onSend={handleSend}
                  onCancel={handleCancel}
                  onSave={handleSave}
                />
              </div>
              <Splitter
                orientation="horizontal"
                ratio={editorRatio}
                onRatio={setEditorRatio}
              />
              <div className="flex-1 min-h-0">
                <ActiveResponse tab={active} />
              </div>
            </div>
          ) : (
            <div className="flex-1 flex items-center justify-center text-gray-400 text-sm">
              {formatMessage('按 Ctrl+T 新建请求标签')}
            </div>
          )}
        </main>
      </div>
    </div>
  );
}

// 查找请求中引用了但未在激活环境 / 全局变量中定义的变量名。
// 数据从 react-query 缓存读取（EnvSwitcher 已加载），不额外发起请求。
function findUndefinedVars(
  qc: ReturnType<typeof useQueryClient>,
  workspaceId: string,
  draft: { url: string },
): string[] {
  const refs = collectVarRefs(draft);
  if (refs.length === 0) return [];
  const known = new Set<string>();
  const envs = qc.getQueryData<Environment[]>(['envs', workspaceId]);
  const active = envs?.find((e) => e.isActive);
  for (const v of active?.variables ?? []) {
    if (v.enabled && v.key) known.add(v.key);
  }
  const globals = qc.getQueryData<Variable[]>(['globals', workspaceId]);
  for (const v of globals ?? []) {
    if (v.enabled && v.key) known.add(v.key);
  }
  return refs.filter((name) => !known.has(name));
}
