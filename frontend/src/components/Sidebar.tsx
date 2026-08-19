// 侧栏：集合树 + 历史 两个页签
import { memo, useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listNodes,
  getNode,
  renameNode,
  upsertNode,
  deleteNode,
  moveNodes,
  listHistory,
  getHistory,
  clearHistory,
  exportData,
  exportMirror,
  openNativeDirectory,
  toAppError,
  type Node,
  type NodeSummary,
  type HistorySummary,
  type NodeMove,
} from '../ipc';
import ImportDialog from './ImportDialog';
import RunnerDialog from './RunnerDialog';
import MockPanel from './MockPanel';
import { useTabs } from '../stores/tabs';
import { newDefaultRequest } from '../ipc';
import { formatMessage, useLocale, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';
import { canMoveNodesInto, canMoveNodesToRoot, orderedTopLevelSelection } from '../utils/treeMove';

// 导出格式选项：value 对应后端 convert.Export(format, ...) 注册名
const EXPORT_FORMATS = [
  { value: 'postman', label: 'Postman v2.1' },
  { value: 'openapi', label: 'OpenAPI 3.0.3' },
  { value: 'openapi3.1', label: 'OpenAPI 3.1.0' },
  { value: 'swagger2', label: 'Swagger 2.0' },
  { value: 'curl', label: 'cURL' },
];

// 稳定的空数组：作为 nodesQuery.data 的兜底值，避免每次渲染产生新引用
const EMPTY_NODES: NodeSummary[] = [];

interface Props {
  workspaceId: string;
}

// memo：App 在编辑请求草稿时频繁重渲染，Sidebar 仅依赖 workspaceId，无需跟着重渲染。
// 但 translateExact 读的是 useLocale 快照而非响应式值，memo 会拦住语言切换导致的重渲染，
// 故显式订阅 locale（与 ResponseViewer 同做法），保证切换语言时文案跟着更新。
const Sidebar = memo(function Sidebar({ workspaceId }: Props) {
  useLocale((state) => state.locale);
  const [pane, setPane] = useState<'collections' | 'history'>('collections');
  return (
    <div className="flex flex-col h-full border-r bg-gray-50">
      <div className="flex gap-1 px-2 pt-2 text-sm border-b">
        {(
          [
            ['collections', formatMessage('集合')],
            ['history', formatMessage('历史')],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            className={`flex-1 text-center px-3 py-1.5 mb-1 rounded ${
              pane === key
                ? 'bg-gray-200 text-gray-900 font-medium'
                : 'text-gray-500 hover:text-gray-800'
            }`}
            onClick={() => setPane(key)}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="flex-1 overflow-auto">
        {pane === 'collections' ? (
          <CollectionTree key={workspaceId} workspaceId={workspaceId} />
        ) : (
          <HistoryList workspaceId={workspaceId} />
        )}
      </div>
    </div>
  );
});

export default Sidebar;

// ── 集合树 ──

function CollectionTree({ workspaceId }: { workspaceId: string }) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const openNode = useTabs((s) => s.openNode);
  const detachNodes = useTabs((s) => s.detachNodes);
  const [importing, setImporting] = useState(false);
  const [runnerTarget, setRunnerTarget] = useState<NodeSummary | null>(null);
  const [mockTarget, setMockTarget] = useState<NodeSummary | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  // 拖拽：HTML5 DnD，仅在同级集合/文件夹内重排；跨层级拖入 folder 时复用同 move
  const [dragId, setDragId] = useState<string | null>(null);
  const dragIdRef = useRef<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);
  const [dragOverBefore, setDragOverBefore] = useState(true);
  // 批量选择
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const lastSelectedId = useRef<string | null>(null);
  const dragSelectedIdsRef = useRef<string[]>([]);
  // 右键菜单
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; node: NodeSummary } | null>(null);
  const nodesQuery = useQuery({
    queryKey: ['nodes', workspaceId],
    queryFn: () => listNodes(workspaceId),
  });
  // 常量兜底：data 缺省时若每次渲染新建 []，依赖 nodes 的 effect 会每次渲染都重跑
  const nodes = nodesQuery.data ?? EMPTY_NODES;

  const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);

  useEffect(() => {
    if (!nodesQuery.isSuccess) return;
    const liveNodeIds = new Set(nodes.map((node) => node.id));
    const staleNodeIds = (useTabs.getState().sessions[workspaceId]?.tabs ?? [])
      .map((tab) => tab.nodeId)
      .filter((nodeId): nodeId is string => typeof nodeId === 'string')
      .filter((nodeId) => !liveNodeIds.has(nodeId));
    detachNodes(workspaceId, staleNodeIds);
  }, [detachNodes, nodes, nodesQuery.isSuccess, workspaceId]);
  const childrenByParent = useMemo(() => {
    const grouped = new Map<string, NodeSummary[]>();
    for (const node of nodes) {
      const siblings = grouped.get(node.parentId ?? '') ?? [];
      siblings.push(node);
      grouped.set(node.parentId ?? '', siblings);
    }
    for (const siblings of grouped.values()) {
      siblings.sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt - b.createdAt);
    }
    return grouped;
  }, [nodes]);
  const childrenOf = (id: string) => childrenByParent.get(id) ?? [];

  // 统计集合下所有 request 后代数量（含多层文件夹）
  const countRequests = useMemo(() => {
    const cache = new Map<string, number>();
    const calc = (id: string): number => {
      if (cache.has(id)) return cache.get(id)!;
      let n = 0;
      for (const c of childrenOf(id)) {
        if (c.kind === 'request') n++;
        else if (c.kind === 'folder') n += calc(c.id);
      }
      cache.set(id, n);
      return n;
    };
    return calc;
  }, [childrenByParent]);

  const invalidate = () => qc.invalidateQueries({ queryKey: ['nodes', workspaceId] });

  // 右键菜单：点击/再次右键时关闭
  useEffect(() => {
    if (!ctxMenu) return;
    const close = () => setCtxMenu(null);
    document.addEventListener('click', close);
    document.addEventListener('contextmenu', close);
    return () => {
      document.removeEventListener('click', close);
      document.removeEventListener('contextmenu', close);
    };
  }, [ctxMenu]);

  const createCollection = useMutation({
    mutationFn: () =>
      upsertNode({
        workspaceId,
        kind: 'collection',
        name: formatMessage('新集合 {index}', {
          index: nodes.filter((n) => n.kind === 'collection').length + 1,
        }),
      } as Node),
    onSuccess: invalidate,
    onError: (cause) => void dialog.alert(
      formatMessage('创建集合失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('创建失败') },
    ),
  });

  const del = useMutation({
    mutationFn: (nodeId: string) => deleteNode(workspaceId, nodeId),
    onSuccess: invalidate,
    onError: (cause) => void dialog.alert(
      formatMessage('删除节点失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('删除失败') },
    ),
  });

  const addChild = useMutation({
    mutationFn: ({ parentId, kind }: { parentId: string; kind: 'request' | 'folder' }) =>
      upsertNode({
        workspaceId,
        parentId,
        kind,
        name: kind === 'folder' ? formatMessage('新文件夹') : formatMessage('新请求'),
        ...(kind === 'request' ? { request: newDefaultRequest() } : {}),
      } as unknown as Node),
    onSuccess: (created) => {
      invalidate();
      if (created.kind === 'request' && created.request) {
        openNode(workspaceId, created.id, created.name, created.request);
      }
    },
    onError: (cause) => void dialog.alert(
      formatMessage('创建节点失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('创建失败') },
    ),
  });

  const rename = useMutation({
    mutationFn: (n: NodeSummary) => renameNode(workspaceId, n.id, n.name),
    onSuccess: invalidate,
    onError: (cause) => void dialog.alert(
      formatMessage('重命名失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('重命名失败') },
    ),
  });

  const move = useMutation({
    mutationFn: (moves: NodeMove[]) => moveNodes(workspaceId, moves),
    onSuccess: invalidate,
    onError: (e) =>
      void dialog.alert(formatMessage('移动失败: {detail}', { detail: toAppError(e).detail }), {
        title: '移动失败',
      }),
  });

  const draggedIds = () => orderedTopLevelSelection(
    nodes,
    dragSelectedIdsRef.current.length > 0
      ? dragSelectedIdsRef.current
      : (dragIdRef.current ? [dragIdRef.current] : []),
  );

  const finishDrag = () => {
    dragIdRef.current = null;
    dragSelectedIdsRef.current = [];
    setDragId(null);
    setDragOverId(null);
  };

  // 节点选中高亮：selected 优先；focus 用更克制的灰色描边，避免两套蓝混淆。
  const nodeHighlightClass = (id: string) =>
    selectedIds.has(id)
      ? 'bg-blue-100 ring-1 ring-blue-300'
      : focusedId === id
        ? 'bg-gray-100 ring-1 ring-gray-300'
        : 'hover:bg-gray-100';

  // 批量选择：Ctrl/Cmd+点击切换选中，Shift+点击范围选择，普通单击选中单节点。
  // 选择必须在 mousedown 阶段完成：树节点带 draggable，若在 click 阶段处理，
  // 按下后指针有轻微位移就会触发原生拖拽并把 click 吞掉（多选失灵）。
  // 范围顺序复用 flatVisible（与键盘/Ctrl+A 一致）；无锚点时 Shift 退化为单选立锚。
  const handleNodeMouseDown = (node: NodeSummary, e: React.MouseEvent) => {
    if (e.ctrlKey || e.metaKey) {
      e.preventDefault();
      e.stopPropagation();
      setSelectedIds((prev) => {
        const next = new Set(prev);
        if (next.has(node.id)) next.delete(node.id);
        else next.add(node.id);
        return next;
      });
      lastSelectedId.current = node.id;
      setFocusedId(node.id);
    } else if (e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      const anchor = lastSelectedId.current;
      if (!anchor) {
        setSelectedIds(new Set([node.id]));
        lastSelectedId.current = node.id;
        setFocusedId(node.id);
        return;
      }
      const startIdx = flatVisible.indexOf(anchor);
      const endIdx = flatVisible.indexOf(node.id);
      if (startIdx >= 0 && endIdx >= 0) {
        const [from, to] = startIdx < endIdx ? [startIdx, endIdx] : [endIdx, startIdx];
        setSelectedIds(new Set(flatVisible.slice(from, to + 1)));
      } else {
        setSelectedIds(new Set([node.id]));
        lastSelectedId.current = node.id;
      }
      setFocusedId(node.id);
    }
  };

  // click 阶段处理普通单击（选中当前节点 + 默认动作）；带修饰键的点击，
  // 选择已在 mousedown 完成，这里直接忽略以免二次切换。
  // 资源管理器式交互：单击=选中，双击=打开（请求）。
  const handleNodeClick = (node: NodeSummary, e: React.MouseEvent, defaultAction?: () => void) => {
    if (e.ctrlKey || e.metaKey || e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    setSelectedIds(new Set([node.id]));
    lastSelectedId.current = node.id;
    setFocusedId(node.id);
    defaultAction?.();
  };

  const batchDelete = async () => {
    const ids = orderedTopLevelSelection(nodes, Array.from(selectedIds));
    if (ids.length === 0) return;
    if (!(await dialog.confirm(formatMessage('删除选中的 {count} 项？', { count: ids.length })))) return;
    const results = await Promise.allSettled(ids.map((id) => deleteNode(workspaceId, id)));
    const failed = results.filter((r) => r.status === 'rejected').length;
    invalidate();
    setSelectedIds(new Set());
    if (failed > 0) {
      void dialog.alert(
        formatMessage('删除完成：成功 {ok}，失败 {failed}', {
          ok: ids.length - failed,
          failed,
        }),
      );
    }
  };

  // folder/request 通用拖拽起点 props
  const dragProps = (n: NodeSummary) => ({
    draggable: true,
    onDragStart: (e: DragEvent<HTMLDivElement>) => {
      e.stopPropagation();
      dragIdRef.current = n.id;
      setDragId(n.id);
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', n.id);
      dragSelectedIdsRef.current = selectedIds.has(n.id) ? Array.from(selectedIds) : [n.id];
    },
    onDragEnd: () => {
      dragIdRef.current = null;
      dragSelectedIdsRef.current = [];
      setDragId(null);
      setDragOverId(null);
    },
  });

  const canMoveInto = (parent: NodeSummary) => {
    return canMoveNodesInto(nodes, draggedIds(), parent.id);
  };
  // 同级排序：拖到目标之前插入
  const canReorderBefore = (target: NodeSummary) => {
    const ids = draggedIds();
    if (ids.length === 0 || ids.includes(target.id)) return false;
    return ids.every((id) => (nodeById.get(id)?.parentId || '') === (target.parentId || ''));
  };
  const reorderSelection = (target: NodeSummary, after: boolean) => {
    const ids = draggedIds();
    if (ids.length === 0) return;
    const selected = new Set(ids);
    const siblings = childrenOf(target.parentId || '');
    const selectedNodes = siblings.filter((node) => selected.has(node.id));
    const remaining = siblings.filter((node) => !selected.has(node.id));
    const targetIndex = remaining.findIndex((node) => node.id === target.id);
    if (targetIndex < 0) return;
    remaining.splice(targetIndex + (after ? 1 : 0), 0, ...selectedNodes);
    move.mutate(remaining.map((node, sortOrder) => ({
      id: node.id,
      parentId: target.parentId || '',
      sortOrder,
    })));
    finishDrag();
  };

  const moveInto = (parent: NodeSummary) => {
    const ids = dragSelectedIdsRef.current.length > 0 ? dragSelectedIdsRef.current : (dragIdRef.current ? [dragIdRef.current] : []);
    const topLevelIds = orderedTopLevelSelection(nodes, ids);
    if (!canMoveNodesInto(nodes, topLevelIds, parent.id)) return;
    const siblings = childrenOf(parent.id);
    let sortOrder = (siblings[siblings.length - 1]?.sortOrder ?? -1) + 1;
    move.mutate(topLevelIds.map((id) => ({ id, parentId: parent.id, sortOrder: sortOrder++ })));
    finishDrag();
  };

  const doRename = async (n: NodeSummary) => {
    const name = await dialog.prompt('重命名：', { defaultValue: n.name });
    if (name && name !== n.name) rename.mutate({ ...n, name });
  };

  const openRequest = async (summary: NodeSummary) => {
    try {
      const node = await getNode(summary.workspaceId, summary.id);
      if (node.request) openNode(node.workspaceId, node.id, node.name, node.request);
    } catch (error) {
      void dialog.alert(
        formatMessage('加载请求失败：{detail}', { detail: toAppError(error).detail }),
        { title: '请求加载失败' },
      );
    }
  };

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  // ── 集合树搜索 ──
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  // 300ms 防抖，参考 HistoryList 的搜索实现
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 300);
    return () => clearTimeout(t);
  }, [search]);
  const searchLower = debouncedSearch.trim().toLowerCase();

  // 搜索时：匹配节点（name 或 method）+ 其全部祖先。仅渲染这些节点。
  const visibleSet = useMemo(() => {
    if (!searchLower) return null;
    const visible = new Set<string>();
    for (const node of nodes) {
      const name = node.name.toLowerCase();
      const method = (node.method ?? '').toLowerCase();
      if (name.includes(searchLower) || method.includes(searchLower)) {
        visible.add(node.id);
        let cur: string | undefined = node.parentId;
        const seen = new Set<string>();
        while (cur && !seen.has(cur)) {
          visible.add(cur);
          seen.add(cur);
          cur = nodeById.get(cur)?.parentId || undefined;
        }
      }
    }
    return visible;
  }, [nodes, nodeById, searchLower]);

  // 搜索过滤后的子节点
  const effectiveChildrenOf = (id: string) => {
    const all = childrenOf(id);
    if (!visibleSet) return all;
    return all.filter((c) => visibleSet.has(c.id));
  };

  // 搜索时强制展开所有可见节点（祖先路径自动展开）
  const isExpanded = (id: string) => {
    if (searchLower) return true;
    return !collapsed.has(id);
  };

  // ── 键盘导航 ──
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const treeRef = useRef<HTMLDivElement>(null);

  // 扁平化当前可见节点列表（深度优先，与渲染顺序一致）
  const flatVisible = useMemo(() => {
    const ids: string[] = [];
    const walk = (parentId: string) => {
      const all = childrenByParent.get(parentId) ?? [];
      const filtered = visibleSet ? all.filter((c) => visibleSet.has(c.id)) : all;
      for (const c of filtered) {
        ids.push(c.id);
        const expanded = searchLower ? true : !collapsed.has(c.id);
        if ((c.kind === 'collection' || c.kind === 'folder') && expanded) {
          walk(c.id);
        }
      }
    };
    walk('');
    return ids;
  }, [childrenByParent, collapsed, searchLower, visibleSet]);

  // 聚焦节点滚动入视
  useEffect(() => {
    if (!focusedId || !treeRef.current) return;
    const el = treeRef.current.querySelector(`[data-node-id="${CSS.escape(focusedId)}"]`);
    if (el instanceof HTMLElement && typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ block: 'nearest' });
    }
  }, [focusedId]);

  const onTreeKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    // 批量选择快捷键：Esc 取消选择、Delete 批量删除、Ctrl+A 全选、Shift+↑/↓ 扩展选区
    if (e.key === 'Escape' && selectedIds.size > 0) {
      setSelectedIds(new Set());
      e.preventDefault();
      return;
    }
    if ((e.ctrlKey || e.metaKey) && (e.key === 'a' || e.key === 'A') && flatVisible.length > 0) {
      e.preventDefault();
      setSelectedIds(new Set(flatVisible));
      if (flatVisible.length > 0) lastSelectedId.current = flatVisible[0];
      return;
    }
    if ((e.key === 'Delete' || e.key === 'Backspace') && selectedIds.size > 0) {
      e.preventDefault();
      void batchDelete();
      return;
    }
    if (flatVisible.length === 0) return;
    const focusNode = (id: string) => {
      setFocusedId(id);
      e.preventDefault();
    };
    // 当前无聚焦或聚焦节点已被过滤掉：方向键聚焦首个
    if (!focusedId || !flatVisible.includes(focusedId)) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        const first = flatVisible[0];
        setFocusedId(first);
        if (e.shiftKey) {
          setSelectedIds(new Set([first]));
          lastSelectedId.current = first;
        }
        e.preventDefault();
      }
      return;
    }
    const idx = flatVisible.indexOf(focusedId);
    const node = nodeById.get(focusedId);
    const moveFocusWithOptionalRange = (nextId: string) => {
      focusNode(nextId);
      if (!e.shiftKey) return;
      const anchor = lastSelectedId.current ?? focusedId;
      if (!lastSelectedId.current) lastSelectedId.current = focusedId;
      const startIdx = flatVisible.indexOf(anchor);
      const endIdx = flatVisible.indexOf(nextId);
      if (startIdx >= 0 && endIdx >= 0) {
        const [from, to] = startIdx < endIdx ? [startIdx, endIdx] : [endIdx, startIdx];
        setSelectedIds(new Set(flatVisible.slice(from, to + 1)));
      }
    };
    switch (e.key) {
      case 'ArrowDown':
        moveFocusWithOptionalRange(flatVisible[Math.min(idx + 1, flatVisible.length - 1)]);
        break;
      case 'ArrowUp':
        moveFocusWithOptionalRange(flatVisible[Math.max(idx - 1, 0)]);
        break;
      case 'ArrowRight':
        e.preventDefault();
        if (node && (node.kind === 'collection' || node.kind === 'folder')) {
          // 折叠态先展开；已展开则进入第一个子节点
          if (!searchLower && collapsed.has(node.id)) {
            toggle(node.id);
          } else {
            const kids = effectiveChildrenOf(node.id);
            if (kids.length > 0) focusNode(kids[0].id);
          }
        }
        break;
      case 'ArrowLeft':
        e.preventDefault();
        if (
          node
          && (node.kind === 'collection' || node.kind === 'folder')
          && !searchLower
          && !collapsed.has(node.id)
        ) {
          // 展开态先折叠
          toggle(node.id);
        } else if (node?.parentId) {
          // 已折叠则移动到父节点
          focusNode(node.parentId);
        }
        break;
      case 'Enter':
        e.preventDefault();
        if (node?.kind === 'request') {
          void openRequest(node);
        } else if (node && (node.kind === 'collection' || node.kind === 'folder') && !searchLower) {
          toggle(node.id);
        }
        break;
      case ' ':
        e.preventDefault();
        if (node) {
          setSelectedIds((prev) => {
            const next = new Set(prev);
            if (next.has(node.id)) next.delete(node.id);
            else next.add(node.id);
            return next;
          });
          lastSelectedId.current = node.id;
        }
        break;
    }
  };

  // 渲染单个 folder/request 节点
  const renderNode = (n: NodeSummary, depth: number): React.JSX.Element =>
    n.kind === 'folder' ? (
        <div key={n.id}>
          <div
            {...dragProps(n)}
            data-node-id={n.id}
            className={`relative flex items-center group py-1 rounded cursor-pointer ${nodeHighlightClass(n.id)} ${
              dragOverId === n.id
                ? canReorderBefore(n)
                  ? dragOverBefore
                    ? "before:content-[''] before:absolute before:inset-x-1 before:top-0 before:h-0.5 before:rounded-full before:bg-blue-500"
                    : "before:content-[''] before:absolute before:inset-x-1 before:bottom-0 before:h-0.5 before:rounded-full before:bg-blue-500"
                  : 'bg-blue-50 ring-1 ring-blue-300'
                : ''
            } ${dragId === n.id ? 'opacity-50' : ''}`}
            style={{ paddingLeft: `${depth * 14 + 4}px` }}
            onDragOver={(e) => {
              if (canMoveInto(n) || canReorderBefore(n)) {
                e.preventDefault();
                e.stopPropagation();
                if (canReorderBefore(n)) {
                  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
                  setDragOverBefore(e.clientY < rect.top + rect.height / 2);
                }
                setDragOverId(n.id);
              }
            }}
            onDragLeave={() => setDragOverId((id) => (id === n.id ? null : id))}
            onDrop={(e) => {
              e.preventDefault();
              e.stopPropagation();
              if (canReorderBefore(n)) {
                reorderSelection(n, !dragOverBefore);
              } else if (canMoveInto(n)) {
                moveInto(n);
              }
            }}
            onMouseDown={(e) => handleNodeMouseDown(n, e)}
            onClick={(e) => handleNodeClick(n, e)}
            onContextMenu={(e) => {
              e.preventDefault();
              e.stopPropagation();
              if (!selectedIds.has(n.id)) setSelectedIds(new Set([n.id]));
              setCtxMenu({ x: e.clientX, y: e.clientY, node: n });
            }}
          >
            <button
              type="button"
              className="mr-0.5 w-4 shrink-0 text-center text-gray-500 hover:text-gray-800"
              title={isExpanded(n.id) ? formatMessage('折叠') : formatMessage('展开')}
              onClick={(e) => {
                e.stopPropagation();
                if (!searchLower) toggle(n.id);
              }}
            >
              {isExpanded(n.id) ? '▾' : '▸'}
            </button>
            <span className="flex-1 truncate text-gray-700">
              {!isExpanded(n.id) ? '📁' : '📂'} <Verbatim value={n.name} />
              {(() => {
                const n2 = countRequests(n.id);
                return n2 > 0 ? <span className="ml-1 text-xs text-gray-400">({n2})</span> : null;
              })()}
            </span>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1"
              title="添加请求"
              onClick={(e) => {
                e.stopPropagation();
                addChild.mutate({ parentId: n.id, kind: 'request' });
              }}
            >
              +
            </button>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="添加子文件夹"
              onClick={(e) => {
                e.stopPropagation();
                addChild.mutate({ parentId: n.id, kind: 'folder' });
              }}
            >
              📁+
            </button>
            <button
              className="hidden group-hover:inline text-gray-400 hover:text-red-500 px-1"
              title="删除"
              onClick={(e) => {
                e.stopPropagation();
                void dialog.confirm(formatMessage('删除文件夹「{name}」及其内容？', { name: n.name })).then((ok) => {
                  if (ok) del.mutate(n.id);
                });
              }}
            >
              ×
            </button>
          </div>
          {isExpanded(n.id) && renderChildren(n.id, depth + 1)}
        </div>
      ) : (
        <TreeLeaf
          key={n.id}
          node={n}
          depth={depth}
          isFocused={focusedId === n.id}
          isDragging={dragId === n.id}
          dragOver={dragOverId === n.id}
          dragOverBefore={dragOverBefore}
          isSelected={selectedIds.has(n.id)}
          onSelect={(e) => handleNodeClick(n, e)}
          onMouseDownNode={(e) => handleNodeMouseDown(n, e)}
          onDelete={(id) => del.mutate(id)}
          onRename={() => doRename(n)}
          onOpen={() => void openRequest(n)}
          onContextMenu={(x, y, node) => {
            if (!selectedIds.has(node.id)) setSelectedIds(new Set([node.id]));
            setCtxMenu({ x, y, node });
          }}
          onDragStart={(e: DragEvent<HTMLDivElement>) => {
            e.stopPropagation();
            dragIdRef.current = n.id;
            setDragId(n.id);
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', n.id);
            dragSelectedIdsRef.current = selectedIds.has(n.id) ? Array.from(selectedIds) : [n.id];
          }}
          onDragEnd={() => {
            finishDrag();
          }}
          onDragOver={(e) => {
            if (canReorderBefore(n)) {
              e.preventDefault();
              e.stopPropagation();
              const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
              setDragOverBefore(e.clientY < rect.top + rect.height / 2);
              setDragOverId(n.id);
            }
          }}
          onDrop={(e) => {
            e.preventDefault();
            e.stopPropagation();
            if (canReorderBefore(n)) {
              reorderSelection(n, !dragOverBefore);
            }
          }}
        />
      );

  // 递归渲染 folder/request
  const renderChildren = (parentId: string, depth: number) =>
    effectiveChildrenOf(parentId).map((n) => renderNode(n, depth));

  const visibleRoots = effectiveChildrenOf('');

  return (
    <div className="p-2 text-sm">
      <div className="flex gap-1 mb-2">
        <button
          className="flex-1 border rounded py-1.5 text-gray-500 hover:text-gray-800 hover:bg-gray-100 hover:border-gray-300"
          onClick={() => createCollection.mutate()}
        >
          + {formatMessage('新建集合')}
        </button>
        <button
          className="border rounded py-1.5 px-3 text-gray-500 hover:text-gray-800 hover:bg-gray-100 hover:border-gray-300"
          onClick={() => setImporting(true)}
          title={formatMessage('导入 Postman / OpenAPI / cURL / HAR / Insomnia')}
        >
          {formatMessage('导入')}
        </button>
      </div>
      <input
        className="w-full border rounded px-2 py-1 mb-2 text-xs outline-none focus:border-blue-400"
        placeholder={formatMessage('搜索 名称 / 方法…')}
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      {selectedIds.size > 0 ? (
        <div className="flex items-center gap-2 mb-2 px-2 py-1 border-b border-gray-200 text-xs text-gray-600">
          <span className="font-medium text-gray-800">
            {formatMessage('已选 {count} 项', { count: selectedIds.size })}
          </span>
          <span className="text-gray-400 hidden sm:inline">
            {formatMessage('Ctrl 多选 · Shift 范围')}
          </span>
          <button
            className="text-red-600 hover:text-red-700 hover:underline"
            onClick={() => void batchDelete()}
          >
            {formatMessage('删除')}
          </button>
          <button
            className="text-gray-500 hover:text-gray-800 hover:underline ml-auto"
            onClick={() => setSelectedIds(new Set())}
          >
            {formatMessage('取消选择')}
          </button>
        </div>
      ) : (
        <div className="mb-2 px-1 text-[11px] text-gray-400">
          {formatMessage('单击选中 · 双击打开 · Ctrl/Shift 多选')}
        </div>
      )}
      {importing && <ImportDialog workspaceId={workspaceId} onClose={() => setImporting(false)} />}
      {runnerTarget && (
        <RunnerDialog
          workspaceId={workspaceId}
          collectionId={runnerTarget.id}
          collectionName={runnerTarget.name}
          onClose={() => setRunnerTarget(null)}
        />
      )}
      {mockTarget && (
        <MockPanel
          collectionId={mockTarget.id}
          collectionName={mockTarget.name}
          onClose={() => setMockTarget(null)}
        />
      )}
      <div
        ref={treeRef}
        tabIndex={0}
        onKeyDown={onTreeKeyDown}
        onMouseDown={() => treeRef.current?.focus()}
        className="outline-none"
      >
      {visibleRoots.length === 0 && (
        <p className="text-gray-400 text-center py-6 text-xs">
          {searchLower ? formatMessage('无匹配结果') : formatMessage('还没有集合，点击上方创建')}
        </p>
      )}
      {visibleRoots.map((col, idx) => (
        col.kind !== 'collection' ? renderNode(col, 0) : (
        <div
          key={col.id}
          data-node-id={col.id}
          className={`mb-1 rounded ${dragOverId === col.id ? 'ring-1 ring-blue-300 bg-blue-50' : ''} ${dragId === col.id ? 'opacity-50' : ''}`}
          draggable
          onDragStart={(e) => {
            dragIdRef.current = col.id;
            setDragId(col.id);
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', col.id);
            dragSelectedIdsRef.current = selectedIds.has(col.id) ? Array.from(selectedIds) : [col.id];
          }}
          onDragEnd={() => {
            finishDrag();
          }}
          onDragOver={(e) => {
            const dragged = dragIdRef.current ? nodeById.get(dragIdRef.current) : undefined;
            if (!dragged || dragged.id === col.id) return;
            // collection→collection 根级排序；request/folder→collection 移入
            if ((dragged.kind === 'collection' && canReorderBefore(col)) || canMoveInto(col)) {
              e.preventDefault();
              e.stopPropagation();
              if (dragged.kind === 'collection') {
                const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
                setDragOverBefore(e.clientY < rect.top + rect.height / 2);
              }
              setDragOverId(col.id);
            }
          }}
          onDragLeave={() => setDragOverId((v) => (v === col.id ? null : v))}
          onDrop={(e) => {
            e.preventDefault();
            e.stopPropagation();
            setDragOverId(null);
            const id = dragIdRef.current;
            if (!id || id === col.id) return;
            const dragged = nodeById.get(id);
            if (!dragged) return;
            if (dragged.kind === 'collection' && canReorderBefore(col)) {
              reorderSelection(col, !dragOverBefore);
            } else {
              moveInto(col);
            }
            finishDrag();
          }}
        >
          <div
            className={`flex items-center group px-1 py-1 rounded cursor-pointer ${nodeHighlightClass(col.id)}`}
            onClick={(e) => handleNodeClick(col, e)}
            onMouseDown={(e) => handleNodeMouseDown(col, e)}
            onContextMenu={(e) => {
              e.preventDefault();
              e.stopPropagation();
              if (!selectedIds.has(col.id)) setSelectedIds(new Set([col.id]));
              setCtxMenu({ x: e.clientX, y: e.clientY, node: col });
            }}
          >
            <button
              type="button"
              className="mr-0.5 w-4 shrink-0 text-center text-gray-500 hover:text-gray-800"
              title={isExpanded(col.id) ? formatMessage('折叠') : formatMessage('展开')}
              onClick={(e) => {
                e.stopPropagation();
                if (!searchLower) toggle(col.id);
              }}
            >
              {isExpanded(col.id) ? '▾' : '▸'}
            </button>
            <span className="font-medium flex-1 truncate">
              {!isExpanded(col.id) ? '📁' : '📂'} <Verbatim value={col.name} />
              {(() => {
                const n = countRequests(col.id);
                return n > 0 ? <span className="ml-1 text-xs text-gray-400">({n})</span> : null;
              })()}
            </span>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1"
              title="添加请求"
              onClick={(e) => {
                e.stopPropagation();
                addChild.mutate({ parentId: col.id, kind: 'request' });
              }}
            >
              +
            </button>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="添加文件夹"
              onClick={(e) => {
                e.stopPropagation();
                addChild.mutate({ parentId: col.id, kind: 'folder' });
              }}
            >
              📁+
            </button>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="Runner 批量运行"
              onClick={(e) => {
                e.stopPropagation();
                setRunnerTarget(col);
              }}
            >
              ▶
            </button>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="Mock Server"
              onClick={(e) => {
                e.stopPropagation();
                setMockTarget(col);
              }}
            >
              M
            </button>
            <ExportButton colId={col.id} count={countRequests(col.id)} />
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="导出为 Git 友好目录镜像"
              onClick={async (e) => {
                e.stopPropagation();
                try {
                  const dir = await openNativeDirectory('选择镜像导出目录');
                  if (!dir) return;
                  await exportMirror(col.id, dir);
                  void dialog.alert(formatMessage('已导出镜像到 {directory}', { directory: dir }), { title: '导出完成' });
                } catch (err) {
                  void dialog.alert(formatMessage('导出失败: {detail}', { detail: toAppError(err).detail }), { title: '导出失败' });
                }
              }}
            >
              ⎘
            </button>
            <button
              className="hidden group-hover:inline text-gray-400 hover:text-red-500 px-1"
              title="删除集合"
              onClick={(e) => {
                e.stopPropagation();
                void dialog.confirm(formatMessage('删除集合「{name}」及其全部请求？', { name: col.name })).then((ok) => {
                  if (ok) del.mutate(col.id);
                });
              }}
            >
              ×
            </button>
          </div>
          {isExpanded(col.id) && renderChildren(col.id, 1)}
        </div>
        )
      ))}
      {/* 根级 drop zone：把节点从集合/文件夹拖出到根级 */}
      <div
        className={`mt-1 min-h-[20px] rounded transition-colors ${
          dragOverId === '__root__' ? 'bg-blue-50 ring-1 ring-blue-300' : ''
        }`}
        onDragOver={(e) => {
          const dragged = dragIdRef.current ? nodeById.get(dragIdRef.current) : undefined;
          if (dragged && canMoveNodesToRoot(nodes, draggedIds())) {
            e.preventDefault();
            e.stopPropagation();
            setDragOverId('__root__');
          }
        }}
        onDragLeave={() => setDragOverId((v) => (v === '__root__' ? null : v))}
        onDrop={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setDragOverId(null);
          const ids = draggedIds();
          if (!canMoveNodesToRoot(nodes, ids)) return;
          const roots = effectiveChildrenOf('');
          let nextSort = (roots[roots.length - 1]?.sortOrder ?? -1) + 1;
          move.mutate(ids.map((id) => ({ id, parentId: '', sortOrder: nextSort++ })));
          finishDrag();
        }}
      />
      </div>
      {ctxMenu && (
        <CtxMenu
          node={ctxMenu.node}
          x={ctxMenu.x}
          y={ctxMenu.y}
          selectedCount={selectedIds.has(ctxMenu.node.id) && selectedIds.size > 1 ? selectedIds.size : 0}
          onRename={() => { void doRename(ctxMenu.node); setCtxMenu(null); }}
          onAddRequest={() => { addChild.mutate({ parentId: ctxMenu.node.id, kind: 'request' }); setCtxMenu(null); }}
          onAddFolder={() => { addChild.mutate({ parentId: ctxMenu.node.id, kind: 'folder' }); setCtxMenu(null); }}
          onRunner={() => { setRunnerTarget(ctxMenu.node); setCtxMenu(null); }}
          onMock={() => { setMockTarget(ctxMenu.node); setCtxMenu(null); }}
          onBatchDelete={() => { setCtxMenu(null); void batchDelete(); }}
          onDelete={() => {
            const n = ctxMenu.node;
            void dialog.confirm(
              n.kind === 'collection'
                ? formatMessage('删除集合「{name}」及其全部请求？', { name: n.name })
                : n.kind === 'folder'
                  ? formatMessage('删除文件夹「{name}」及其内容？', { name: n.name })
                  : formatMessage('删除「{name}」？', { name: n.name })
            ).then((ok) => {
              if (ok) del.mutate(n.id);
            });
            setCtxMenu(null);
          }}
        />
      )}
    </div>
  );
}

// 右键菜单
function CtxMenu({ node, x, y, selectedCount, onRename, onAddRequest, onAddFolder, onRunner, onMock, onBatchDelete, onDelete }: {
  node: NodeSummary;
  x: number;
  y: number;
  selectedCount: number;
  onRename(): void;
  onAddRequest(): void;
  onAddFolder(): void;
  onRunner(): void;
  onMock(): void;
  onBatchDelete(): void;
  onDelete(): void;
}) {
  const canAddChild = node.kind === 'collection' || node.kind === 'folder';
  const left = Math.min(x, window.innerWidth - 180);
  const top = Math.min(y, window.innerHeight - 240);
  return (
    <div
      className="fixed z-50 min-w-[160px] rounded border border-gray-200 bg-white py-1 text-sm shadow-lg"
      style={{ left, top }}
    >
      {selectedCount > 1 && (
        <>
          <button className="block w-full px-3 py-1.5 text-left text-red-600 hover:bg-red-50" onClick={onBatchDelete}>
            {formatMessage('删除选中的 {count} 项', { count: selectedCount })}
          </button>
          <div className="my-1 border-t border-gray-100" />
        </>
      )}
      <button className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={onRename}>
        {formatMessage('重命名')}
      </button>
      {canAddChild && (
        <>
          <div className="my-1 border-t border-gray-100" />
          <button className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={onAddRequest}>
            {formatMessage('添加请求')}
          </button>
          <button className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={onAddFolder}>
            {formatMessage('添加文件夹')}
          </button>
        </>
      )}
      {node.kind === 'collection' && (
        <>
          <div className="my-1 border-t border-gray-100" />
          <button className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={onRunner}>
            {formatMessage('Runner 批量运行')}
          </button>
          <button className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={onMock}>
            Mock Server
          </button>
        </>
      )}
      <div className="my-1 border-t border-gray-100" />
      <button className="block w-full px-3 py-1.5 text-left text-red-600 hover:bg-red-50" onClick={onDelete}>
        {formatMessage('删除')}
      </button>
    </div>
  );
}

// 导出按钮：点击展开格式下拉菜单，选择后执行导出并复制到剪贴板
function ExportButton({ colId, count }: { colId: string; count: number }) {
  const dialog = useDialog();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as HTMLElement)) setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);

  const doExport = async (fmt: string) => {
    setOpen(false);
    try {
      const out = await exportData(colId, fmt);
      await navigator.clipboard.writeText(out);
      const label = EXPORT_FORMATS.find((f) => f.value === fmt)?.label ?? fmt;
      void dialog.alert(formatMessage('已复制 {label} 到剪贴板', { label }), { title: '导出完成' });
    } catch (err) {
      void dialog.alert(formatMessage('导出失败: {detail}', { detail: toAppError(err).detail }), { title: '导出失败' });
    }
  };

  return (
    <div ref={ref} className="relative inline-block">
      <button
        className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
        title={formatMessage('导出集合')}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
      >
        ⇪
      </button>
      {open && (
        <div className="absolute top-full right-0 mt-1 bg-white border border-gray-200 rounded-lg shadow-lg py-1 z-50 min-w-[140px]">
          <div className="px-3 py-1 text-xs text-gray-400 border-b mb-1">
            {formatMessage('选择导出格式')}
            {count > 0 && <span className="ml-1">· {count} {formatMessage('个请求')}</span>}
          </div>
          {EXPORT_FORMATS.map((f) => (
            <button
              key={f.value}
              className="w-full text-left px-3 py-1.5 text-xs whitespace-nowrap hover:bg-blue-50 text-gray-700"
              onClick={(e) => {
                e.stopPropagation();
                void doExport(f.value);
              }}
            >
              {f.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function TreeLeaf({
  node,
  depth,
  isFocused,
  isDragging,
  dragOver,
  dragOverBefore,
  isSelected,
  onDelete,
  onRename,
  onOpen,
  onDragStart,
  onDragEnd,
  onDragOver,
  onDrop,
  onContextMenu,
  onSelect,
  onMouseDownNode,
}: {
  node: NodeSummary;
  depth: number;
  isFocused?: boolean;
  isDragging?: boolean;
  dragOver?: boolean;
  dragOverBefore?: boolean;
  isSelected?: boolean;
  onDelete(id: string): void;
  onRename(): void;
  onOpen(): void;
  onDragStart?(e: DragEvent<HTMLDivElement>): void;
  onDragEnd?(): void;
  onDragOver?(e: DragEvent<HTMLDivElement>): void;
  onDrop?(e: DragEvent<HTMLDivElement>): void;
  onContextMenu(x: number, y: number, node: NodeSummary): void;
  onSelect?(e: React.MouseEvent): void;
  onMouseDownNode?(e: React.MouseEvent): void;
}) {
  if (node.kind !== 'request') return null;
  return (
    <div
      data-node-id={node.id}
      className={`relative flex items-center group pr-1 py-1 rounded cursor-pointer ${
        isSelected
          ? 'bg-blue-100 ring-1 ring-blue-300'
          : isFocused
            ? 'bg-gray-100 ring-1 ring-gray-300'
            : 'hover:bg-gray-100'
      } ${isDragging ? 'opacity-50' : ''} ${
        dragOver
          ? dragOverBefore
            ? "before:content-[''] before:absolute before:inset-x-1 before:top-0 before:h-0.5 before:rounded-full before:bg-blue-500"
            : "before:content-[''] before:absolute before:inset-x-1 before:bottom-0 before:h-0.5 before:rounded-full before:bg-blue-500"
          : ''
      }`}
      style={{ paddingLeft: `${depth * 14 + 4}px` }}
      draggable
      onDragStart={(e) => onDragStart?.(e)}
      onDragEnd={onDragEnd}
      onDragOver={(e) => onDragOver?.(e)}
      onDrop={(e) => onDrop?.(e)}
      onMouseDown={(e) => onMouseDownNode?.(e)}
      onClick={(e) => {
        if (e.ctrlKey || e.metaKey || e.shiftKey) {
          e.preventDefault();
          e.stopPropagation();
        }
        onSelect?.(e);
      }}
      onDoubleClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onOpen();
      }}
      onContextMenu={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onContextMenu(e.clientX, e.clientY, node);
      }}
    >
      <span className={`text-xs font-semibold w-12 shrink-0 ${methodColor(node.method)}`}>
        {node.method || 'GET'}
      </span>
      <span className="flex-1 truncate"><Verbatim value={node.name} /></span>
      <button
        className="hidden group-hover:inline text-gray-400 hover:text-red-500 px-1"
        onClick={(e) => {
          e.stopPropagation();
          onDelete(node.id);
        }}
      >
        ×
      </button>
    </div>
  );
}

// ── 历史 ──

function HistoryList({ workspaceId }: { workspaceId: string }) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const openBlank = useTabs((s) => s.openBlank);
  const patchDraft = useTabs((s) => s.patchDraft);
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  const [replayingId, setReplayingId] = useState('');

  // 300ms 防抖后再查询
  useEffect(() => {
    const t = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(t);
  }, [search]);

  const history = useInfiniteQuery({
    queryKey: ['history', workspaceId, debounced],
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      listHistory(workspaceId, {
        ...(debounced ? { search: debounced } : {}),
        ...(pageParam ? { cursor: pageParam } : {}),
        limit: 50,
      }),
    getNextPageParam: (lastPage) => (lastPage.hasMore ? lastPage.nextCursor : undefined),
  });
  const items = history.data?.pages.flatMap((page) => page.items) ?? [];

  const clear = useMutation({
    mutationFn: () => clearHistory(workspaceId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['history'] }),
    onError: (cause) => void dialog.alert(
      formatMessage('清空历史失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('清空失败') },
    ),
  });

  const replay = async (item: HistorySummary) => {
    if (replayingId) return;
    setReplayingId(item.id);
    try {
      const detail = await getHistory(workspaceId, item.id);
      const tabId = openBlank(workspaceId);
      patchDraft(tabId, detail.requestSnap);
    } catch (error) {
      void dialog.alert(
        formatMessage('加载历史详情失败：{detail}', { detail: toAppError(error).detail }),
        { title: '历史记录加载失败' },
      );
    } finally {
      setReplayingId('');
    }
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex gap-1 p-2 border-b">
        <input
          className="flex-1 border rounded px-2 py-1 text-xs outline-none focus:border-blue-400"
          placeholder="搜索 URL / 方法…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button
          className="border rounded px-2 py-1 text-xs text-gray-400 hover:text-red-500 hover:border-gray-300 disabled:cursor-not-allowed disabled:opacity-50"
          title={formatMessage('清空全部历史')}
          disabled={clear.isPending}
          onClick={() => {
            void dialog.confirm(formatMessage('清空全部历史记录？')).then((ok) => {
              if (ok) clear.mutate();
            });
          }}
        >
          {clear.isPending ? formatMessage('清空中…') : formatMessage('清空')}
        </button>
      </div>
      <div className="flex-1 overflow-auto">
        {history.isPending ? (
          <p className="text-gray-400 text-center py-6 text-xs">{formatMessage('加载中…')}</p>
        ) : history.isError && items.length === 0 ? (
          <div className="space-y-2 px-3 py-6 text-center text-xs" role="alert">
            <p className="break-words text-red-600">
              <Verbatim
                value={formatMessage('加载历史失败：{detail}', {
                  detail: toAppError(history.error).detail,
                })}
              />
            </p>
            <button
              className="rounded border px-2 py-1 text-gray-600 hover:bg-gray-100"
              onClick={() => void history.refetch()}
            >
              {formatMessage('重试')}
            </button>
          </div>
        ) : items.length === 0 ? (
          <p className="text-gray-400 text-center py-6 text-xs">
            {debounced ? '无匹配记录' : '暂无历史记录'}
          </p>
        ) : (
          <div className="text-sm">
            {items.map((it) => (
              <div
                key={it.id}
                className="px-2 py-1.5 border-b border-gray-100 hover:bg-gray-200 cursor-pointer"
                onClick={() => void replay(it)}
                title="点击重放"
              >
                <div className="flex items-center gap-2">
                  <span className={`text-xs font-semibold ${methodColor(it.method)}`}>
                    <Verbatim value={it.method} />
                  </span>
                  <span className={`text-xs ${it.status < 400 ? 'text-green-600' : 'text-red-600'}`}>
                    {it.status}
                  </span>
                  <span className="text-xs text-gray-400 ml-auto">{formatTime(it.createdAt)}</span>
                </div>
                <div className="truncate text-xs text-gray-600 font-mono"><Verbatim value={it.url} /></div>
              </div>
            ))}
            {history.hasNextPage && !history.isFetchNextPageError && (
              <LoadMoreTrigger
                isFetching={history.isFetchingNextPage}
                onLoadMore={() => void history.fetchNextPage()}
              />
            )}
            {history.isFetchNextPageError && (
              <div className="flex items-center justify-center gap-2 px-2 py-2 text-xs text-red-600" role="alert">
                <span>{formatMessage('加载更多历史失败')}</span>
                <button className="underline" onClick={() => void history.fetchNextPage()}>
                  {formatMessage('重试')}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function methodColor(method?: string): string {
  switch (method) {
    case 'POST':
      return 'text-yellow-600';
    case 'PUT':
      return 'text-blue-600';
    case 'DELETE':
      return 'text-red-600';
    case 'PATCH':
      return 'text-purple-600';
    default:
      return 'text-green-600';
  }
}

// 历史时间：当天显示 HH:MM，跨天显示 M/D HH:MM（含年份不同时加年份）
function formatTime(ms: number): string {
  const d = new Date(ms);
  const now = new Date();
  const hhmm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  const sameDate =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDate) return hhmm;
  return `${d.getMonth() + 1}/${d.getDate()} ${hhmm}`;
}

// 无限滚动触发器：进入视口时自动加载下一页
function LoadMoreTrigger({ isFetching, onLoadMore }: { isFetching: boolean; onLoadMore(): void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && !isFetching) onLoadMore();
      },
      { rootMargin: '100px' },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [isFetching, onLoadMore]);
  return (
    <div ref={ref} className="w-full py-2 text-xs text-center text-gray-400">
      {isFetching ? '加载中…' : '滚动加载更多'}
    </div>
  );
}
