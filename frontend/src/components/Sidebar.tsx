// 侧栏：集合树 + 历史 两个页签
import { useEffect, useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listNodes,
  upsertNode,
  deleteNode,
  listHistory,
  clearHistory,
  exportData,
  exportMirror,
  toAppError,
  type Node,
  type HistoryItem,
} from '../ipc';
import ImportDialog from './ImportDialog';
import RunnerDialog from './RunnerDialog';
import MockPanel from './MockPanel';
import { useTabs } from '../stores/tabs';
import { newDefaultRequest } from '../ipc';

interface Props {
  workspaceId: string;
}

export default function Sidebar({ workspaceId }: Props) {
  const [pane, setPane] = useState<'collections' | 'history'>('collections');
  return (
    <div className="flex flex-col h-full border-r bg-gray-50">
      <div className="flex text-sm border-b">
        {(
          [
            ['collections', '集合'],
            ['history', '历史'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            className={`flex-1 py-2 ${
              pane === key ? 'bg-white font-medium' : 'text-gray-500 hover:text-gray-800'
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
  const qc = useQueryClient();
  const openNode = useTabs((s) => s.openNode);
  const [importing, setImporting] = useState(false);
  const [runnerTarget, setRunnerTarget] = useState<Node | null>(null);
  const [mockTarget, setMockTarget] = useState<Node | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const { data: nodes = [] } = useQuery({
    queryKey: ['nodes', workspaceId],
    queryFn: () => listNodes(workspaceId),
  });

  const invalidate = () => qc.invalidateQueries({ queryKey: ['nodes', workspaceId] });

  const createCollection = useMutation({
    mutationFn: () =>
      upsertNode({
        workspaceId,
        kind: 'collection',
        name: `新集合 ${nodes.filter((n) => n.kind === 'collection').length + 1}`,
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
        openNode(created.id, created.name, created.request);
      }
    },
  });

  const rename = useMutation({
    mutationFn: (n: Node) => upsertNode(n),
    onSuccess: invalidate,
  });

  const doRename = (n: Node) => {
    const name = prompt('重命名：', n.name);
    if (name && name !== n.name) rename.mutate({ ...n, name } as Node);
  };

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const childrenOf = (id: string) =>
    nodes
      .filter((n) => n.parentId === id)
      .sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt - b.createdAt);
  const roots = nodes
    .filter((n) => !n.parentId)
    .sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt - b.createdAt);

  // 递归渲染 folder/request
  const renderChildren = (parentId: string, depth: number) =>
    childrenOf(parentId).map((n) =>
      n.kind === 'folder' ? (
        <div key={n.id}>
          <div
            className="flex items-center group py-1 rounded hover:bg-gray-200 cursor-pointer"
            style={{ paddingLeft: `${depth * 14 + 4}px` }}
            onClick={() => toggle(n.id)}
            onDoubleClick={(e) => {
              e.stopPropagation();
              doRename(n);
            }}
          >
            <span className="flex-1 truncate text-gray-700">
              {collapsed.has(n.id) ? '📁' : '📂'} {n.name}
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
                if (confirm(`删除文件夹「${n.name}」及其内容？`)) del.mutate(n.id);
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
        />
      ),
    );

  return (
    <div className="p-2 text-sm">
      <div className="flex gap-1 mb-2">
        <button
          className="flex-1 border border-dashed rounded py-1.5 text-gray-500 hover:text-gray-800 hover:border-gray-400"
          onClick={() => createCollection.mutate()}
        >
          + 新建集合
        </button>
        <button
          className="border border-dashed rounded py-1.5 px-3 text-gray-500 hover:text-gray-800 hover:border-gray-400"
          onClick={() => setImporting(true)}
          title="导入 Postman / OpenAPI / cURL / HAR / Insomnia"
        >
          导入
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
      {roots.map((col) => (
        <div key={col.id} className="mb-1">
          <div
            className="flex items-center group px-1 py-1 rounded hover:bg-gray-200 cursor-pointer"
            onClick={() => toggle(col.id)}
            onDoubleClick={() => doRename(col)}
          >
            <span className="font-medium flex-1 truncate">
              {collapsed.has(col.id) ? '📁' : '📂'} {col.name}
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
              title="导出为 Postman v2.1 JSON"
              onClick={async (e) => {
                e.stopPropagation();
                const out = await exportData(col.id, 'postman');
                await navigator.clipboard.writeText(out);
                alert('已复制 Postman v2.1 JSON 到剪贴板');
              }}
            >
              ⇪
            </button>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="导出为 Git 友好目录镜像"
              onClick={async (e) => {
                e.stopPropagation();
                const dir = prompt('导出到目录（绝对路径）：', '');
                if (!dir) return;
                try {
                  await exportMirror(col.id, dir);
                  alert(`已导出镜像到 ${dir}`);
                } catch (err) {
                  alert('导出失败: ' + toAppError(err).detail);
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
                if (confirm(`删除集合「${col.name}」及其全部请求？`)) del.mutate(col.id);
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
}: {
  node: Node;
  depth: number;
  onDelete(id: string): void;
  onRename(): void;
}) {
  const openNode = useTabs((s) => s.openNode);
  if (node.kind !== 'request') return null;
  return (
    <div
      className="flex items-center group pr-1 py-1 rounded hover:bg-gray-200 cursor-pointer"
      style={{ paddingLeft: `${depth * 14 + 4}px` }}
      onClick={() => node.request && openNode(node.id, node.name, node.request)}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onRename();
      }}
    >
      <span className={`text-xs font-semibold w-12 shrink-0 ${methodColor(node.request?.method)}`}>
        {node.request?.method ?? 'GET'}
      </span>
      <span className="flex-1 truncate">{node.name}</span>
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
  const qc = useQueryClient();
  const openBlank = useTabs((s) => s.openBlank);
  const patchDraft = useTabs((s) => s.patchDraft);
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');

  // 300ms 防抖后再查询
  useEffect(() => {
    const t = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(t);
  }, [search]);

  const { data: items = [] } = useQuery({
    queryKey: ['history', workspaceId, debounced],
    queryFn: () => listHistory(workspaceId, debounced ? { search: debounced } : {}),
    refetchInterval: debounced ? false : 3000,
  });

  const clear = useMutation({
    mutationFn: () => clearHistory(workspaceId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['history'] }),
  });

  const replay = (item: HistoryItem) => {
    openBlank();
    const { activeId } = useTabs.getState();
    if (activeId) patchDraft(activeId, item.requestSnap);
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
          className="text-xs text-gray-400 hover:text-red-500 px-1"
          title="清空全部历史"
          onClick={() => {
            if (confirm('清空全部历史记录？')) clear.mutate();
          }}
        >
          清空
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
                onClick={() => replay(it)}
                title="点击重放"
              >
                <div className="flex items-center gap-2">
                  <span className={`text-xs font-semibold ${methodColor(it.requestSnap.method)}`}>
                    {it.requestSnap.method}
                  </span>
                  <span className={`text-xs ${it.status < 400 ? 'text-green-600' : 'text-red-600'}`}>
                    {it.status}
                  </span>
                  <span className="text-xs text-gray-400 ml-auto">{formatTime(it.createdAt)}</span>
                </div>
                <div className="truncate text-xs text-gray-600 font-mono">{it.requestSnap.url}</div>
              </div>
            ))}
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
