// 侧栏：集合树 + 历史 两个页签
import { useEffect, useMemo, useState, type DragEvent } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listNodes,
  upsertNode,
  deleteNode,
  moveNode,
  listHistory,
  getHistory,
  clearHistory,
  exportData,
  exportMirror,
  openNativeDirectory,
  toAppError,
  type Node,
  type HistorySummary,
} from '../ipc';
import ImportDialog from './ImportDialog';
import RunnerDialog from './RunnerDialog';
import MockPanel from './MockPanel';
import { useTabs } from '../stores/tabs';
import { newDefaultRequest } from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';

interface Props {
  workspaceId: string;
}

export default function Sidebar({ workspaceId }: Props) {
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
          <CollectionTree workspaceId={workspaceId} />
        ) : (
          <HistoryList workspaceId={workspaceId} />
        )}
      </div>
    </div>
  );
}

// ── 集合树 ──

function CollectionTree({ workspaceId }: { workspaceId: string }) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const openNode = useTabs((s) => s.openNode);
  const [importing, setImporting] = useState(false);
  const [runnerTarget, setRunnerTarget] = useState<Node | null>(null);
  const [mockTarget, setMockTarget] = useState<Node | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  // 拖拽：HTML5 DnD，仅在同级集合/文件夹内重排；跨层级拖入 folder 时复用同 move
  const [dragId, setDragId] = useState<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);
  const { data: nodes = [] } = useQuery({
    queryKey: ['nodes', workspaceId],
    queryFn: () => listNodes(workspaceId),
  });

  const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
  const childrenByParent = useMemo(() => {
    const grouped = new Map<string, Node[]>();
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
  const roots = childrenOf('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['nodes', workspaceId] });

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
  });

  const del = useMutation({
    mutationFn: (nodeId: string) => deleteNode(nodeId),
    onSuccess: invalidate,
  });

  const addChild = useMutation({
    mutationFn: ({ parentId, kind }: { parentId: string; kind: 'request' | 'folder' }) =>
      upsertNode({
        workspaceId,
        parentId,
        kind,
        name: kind === 'folder' ? '新文件夹' : '新请求',
        ...(kind === 'request' ? { request: newDefaultRequest() } : {}),
      } as unknown as Node),
    onSuccess: (created) => {
      invalidate();
      if (created.kind === 'request' && created.request) {
        openNode(workspaceId, created.id, created.name, created.request);
      }
    },
  });

  const rename = useMutation({
    mutationFn: (n: Node) => upsertNode(n),
    onSuccess: invalidate,
  });

  const move = useMutation({
    // newParentId 为空字符串表示拖到根
    mutationFn: ({ id, parentId, sortOrder }: { id: string; parentId: string; sortOrder: number }) =>
      moveNode(id, parentId, sortOrder),
    onSuccess: invalidate,
    onError: (e) =>
      void dialog.alert(formatMessage('移动失败: {detail}', { detail: toAppError(e).detail }), {
        title: '移动失败',
      }),
  });

  // 判断 descendant 是否为 ancestor 的后代（含多层），防止把文件夹拖进自身子树造成环路。
  const isDescendant = (ancestor: string, descendant: string): boolean => {
    let cur: string | undefined = descendant;
    const seen = new Set<string>();
    while (cur && !seen.has(cur)) {
      if (cur === ancestor) return true;
      seen.add(cur);
      const node = nodeById.get(cur);
      cur = node?.parentId || undefined;
    }
    return false;
  };

  // folder/request 通用拖拽起点 props
  const dragProps = (n: Node) => ({
    draggable: true,
    onDragStart: (e: DragEvent<HTMLDivElement>) => {
      e.stopPropagation();
      setDragId(n.id);
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', n.id);
    },
    onDragEnd: () => {
      setDragId(null);
      setDragOverId(null);
    },
  });

  const canMoveInto = (parent: Node) => {
    const dragged = dragId ? nodeById.get(dragId) : undefined;
    return !!dragged
      && dragged.kind !== 'collection'
      && dragged.id !== parent.id
      && !isDescendant(dragged.id, parent.id);
  };

  const moveInto = (parent: Node) => {
    const dragged = dragId ? nodeById.get(dragId) : undefined;
    if (!dragged || !canMoveInto(parent)) return;
    const siblings = childrenOf(parent.id);
    const nextSort = (siblings[siblings.length - 1]?.sortOrder ?? -1) + 1;
    move.mutate({ id: dragged.id, parentId: parent.id, sortOrder: nextSort });
    setDragId(null);
    setDragOverId(null);
  };

  const doRename = async (n: Node) => {
    const name = await dialog.prompt('重命名：', { defaultValue: n.name });
    if (name && name !== n.name) rename.mutate({ ...n, name } as Node);
  };

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  // 递归渲染 folder/request
  const renderChildren = (parentId: string, depth: number) =>
    childrenOf(parentId).map((n) =>
      n.kind === 'folder' ? (
        <div key={n.id}>
          <div
            {...dragProps(n)}
            className={`flex items-center group py-1 rounded hover:bg-gray-200 cursor-pointer ${dragOverId === n.id ? 'bg-blue-50 ring-1 ring-blue-300' : ''}`}
            style={{ paddingLeft: `${depth * 14 + 4}px` }}
            onDragOver={(e) => {
              if (canMoveInto(n)) {
                e.preventDefault();
                e.stopPropagation();
                setDragOverId(n.id);
              }
            }}
            onDragLeave={() => setDragOverId((id) => (id === n.id ? null : id))}
            onDrop={(e) => {
              e.preventDefault();
              e.stopPropagation();
              moveInto(n);
            }}
            onClick={() => toggle(n.id)}
            onDoubleClick={(e) => {
              e.stopPropagation();
              doRename(n);
            }}
          >
            <span className="flex-1 truncate text-gray-700">
              {collapsed.has(n.id) ? '📁' : '📂'} <Verbatim value={n.name} />
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
          {!collapsed.has(n.id) && renderChildren(n.id, depth + 1)}
        </div>
      ) : (
        <TreeLeaf
          key={n.id}
          node={n}
          depth={depth}
          onDelete={(id) => del.mutate(id)}
          onRename={() => doRename(n)}
          onDragStart={(e: DragEvent<HTMLDivElement>) => {
            e.stopPropagation();
            setDragId(n.id);
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', n.id);
          }}
          onDragEnd={() => {
            setDragId(null);
            setDragOverId(null);
          }}
        />
      ),
    );

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
      {roots.length === 0 && (
        <p className="text-gray-400 text-center py-6 text-xs">还没有集合，点击上方创建</p>
      )}
      {roots.map((col, idx) => (
        <div
          key={col.id}
          className={`mb-1 ${dragOverId === col.id ? 'border-t-2 border-blue-400' : ''}`}
          draggable
          onDragStart={(e) => {
            setDragId(col.id);
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', col.id);
          }}
          onDragEnd={() => {
            setDragId(null);
            setDragOverId(null);
          }}
          onDragOver={(e) => {
            const dragged = dragId ? nodeById.get(dragId) : undefined;
            if (dragged?.kind === 'collection' && dragged.id !== col.id) {
              e.preventDefault();
              setDragOverId(col.id);
            }
          }}
          onDragLeave={() => setDragOverId((v) => (v === col.id ? null : v))}
          onDrop={(e) => {
            e.preventDefault();
            e.stopPropagation();
            setDragOverId(null);
            if (!dragId || dragId === col.id) return;
            const dragged = nodeById.get(dragId);
            if (dragged?.kind !== 'collection') return;
            // 插入到当前 col 之前：sortOrder 取前一个 col 与 col 之间的中点
            const nextSort = col.sortOrder;
            const newSort = idx === 0
              ? nextSort - 1
              : (roots[idx - 1].sortOrder + nextSort) / 2;
            move.mutate({ id: dragId, parentId: '', sortOrder: newSort });
            setDragId(null);
          }}
        >
          <div
            className="flex items-center group px-1 py-1 rounded hover:bg-gray-200 cursor-pointer"
            onClick={() => toggle(col.id)}
            onDoubleClick={() => doRename(col)}
          >
            <span className="font-medium flex-1 truncate">
              {collapsed.has(col.id) ? '📁' : '📂'} <Verbatim value={col.name} />
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
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="导出集合"
              onClick={async (e) => {
                e.stopPropagation();
                const fmt = await dialog.prompt('导出格式（postman / openapi / curl）：', { defaultValue: 'postman' });
                if (!fmt) return;
                try {
                  const out = await exportData(col.id, fmt.toLowerCase().trim());
                  await navigator.clipboard.writeText(out);
                  const label = fmt.toLowerCase().trim() === 'openapi' ? 'OpenAPI 3.0.3'
                    : fmt.toLowerCase().trim() === 'curl' ? 'cURL（JSON + shell）'
                    : 'Postman v2.1';
                  void dialog.alert(formatMessage('已复制 {label} 到剪贴板', { label }), { title: '导出完成' });
                } catch (err) {
                  void dialog.alert(formatMessage('导出失败: {detail}', { detail: toAppError(err).detail }), { title: '导出失败' });
                }
              }}
            >
              ⇪
            </button>
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
          {!collapsed.has(col.id) && renderChildren(col.id, 1)}
        </div>
      ))}
    </div>
  );
}

function TreeLeaf({
  node,
  depth,
  onDelete,
  onRename,
  onDragStart,
  onDragEnd,
}: {
  node: Node;
  depth: number;
  onDelete(id: string): void;
  onRename(): void;
  onDragStart?(e: DragEvent<HTMLDivElement>): void;
  onDragEnd?(): void;
}) {
  const openNode = useTabs((s) => s.openNode);
  if (node.kind !== 'request') return null;
  return (
    <div
      className="flex items-center group pr-1 py-1 rounded hover:bg-gray-200 cursor-pointer"
      style={{ paddingLeft: `${depth * 14 + 4}px` }}
      draggable
      onDragStart={(e) => onDragStart?.(e)}
      onDragEnd={onDragEnd}
      onClick={() => node.request && openNode(node.workspaceId, node.id, node.name, node.request)}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onRename();
      }}
    >
      <span className={`text-xs font-semibold w-12 shrink-0 ${methodColor(node.request?.method)}`}>
        {node.request?.method ?? 'GET'}
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
          className="border rounded px-2 py-1 text-xs text-gray-400 hover:text-red-500 hover:border-gray-300"
          title={formatMessage('清空全部历史')}
          onClick={() => {
            void dialog.confirm(formatMessage('清空全部历史记录？')).then((ok) => {
              if (ok) clear.mutate();
            });
          }}
        >
          {formatMessage('清空')}
        </button>
      </div>
      <div className="flex-1 overflow-auto">
        {items.length === 0 ? (
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
            {history.hasNextPage && (
              <button
                className="w-full py-2 text-xs text-gray-500 hover:bg-gray-100 disabled:opacity-50"
                disabled={history.isFetchingNextPage}
                onClick={() => void history.fetchNextPage()}
              >
                {history.isFetchingNextPage ? '加载中…' : '加载更多'}
              </button>
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

function formatTime(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}
