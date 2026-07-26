// 侧栏：集合树 + 历史 两个页签
import { useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listNodes,
  upsertNode,
  deleteNode,
  listHistory,
  exportData,
  type Node,
  type HistoryItem,
} from '../ipc';
import ImportDialog from './ImportDialog';
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
  const { data: nodes = [] } = useQuery({
    queryKey: ['nodes', workspaceId],
    queryFn: () => listNodes(workspaceId),
  });

  const createCollection = useMutation({
    mutationFn: () =>
      upsertNode({
        workspaceId,
        kind: 'collection',
        name: `新集合 ${nodes.filter((n) => n.kind === 'collection').length + 1}`,
      } as Node),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['nodes', workspaceId] }),
  });

  const del = useMutation({
    mutationFn: (nodeId: string) => deleteNode(nodeId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['nodes', workspaceId] }),
  });

  const addRequest = useMutation({
    mutationFn: (parentId: string) =>
      upsertNode({
        workspaceId,
        parentId,
        kind: 'request',
        name: '新请求',
        request: newDefaultRequest(),
      } as unknown as Node),
    onSuccess: (created) => {
      qc.invalidateQueries({ queryKey: ['nodes', workspaceId] });
      if (created.request) openNode(created.id, created.name, created.request);
    },
  });

  const roots = nodes.filter((n) => !n.parentId);
  const childrenOf = (id: string) => nodes.filter((n) => n.parentId === id);

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
          title="导入 Postman / cURL"
        >
          导入
        </button>
      </div>
      {importing && <ImportDialog workspaceId={workspaceId} onClose={() => setImporting(false)} />}
      {roots.length === 0 && (
        <p className="text-gray-400 text-center py-6 text-xs">还没有集合，点击上方创建</p>
      )}
      {roots.map((col) => (
        <div key={col.id} className="mb-1">
          <div className="flex items-center group px-1 py-1 rounded hover:bg-gray-200">
            <span className="font-medium flex-1 truncate">📁 {col.name}</span>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1"
              title="添加请求"
              onClick={() => addRequest.mutate(col.id)}
            >
              +
            </button>
            <button
              className="hidden group-hover:inline text-gray-500 hover:text-gray-800 px-1 text-xs"
              title="导出为 Postman v2.1 JSON"
              onClick={async () => {
                const out = await exportData(col.id, 'postman');
                await navigator.clipboard.writeText(out);
                alert('已复制 Postman v2.1 JSON 到剪贴板');
              }}
            >
              ⇪
            </button>
            <button
              className="hidden group-hover:inline text-gray-400 hover:text-red-500 px-1"
              title="删除集合"
              onClick={() => {
                if (confirm(`删除集合「${col.name}」及其全部请求？`)) del.mutate(col.id);
              }}
            >
              ×
            </button>
          </div>
          {childrenOf(col.id).map((child) => (
            <TreeLeaf key={child.id} node={child} onDelete={(id) => del.mutate(id)} />
          ))}
        </div>
      ))}
    </div>
  );
}

function TreeLeaf({ node, onDelete }: { node: Node; onDelete(id: string): void }) {
  const openNode = useTabs((s) => s.openNode);
  if (node.kind !== 'request') return null; // Phase 1 不做嵌套 folder UI
  return (
    <div
      className="flex items-center group pl-6 pr-1 py-1 rounded hover:bg-gray-200 cursor-pointer"
      onClick={() => node.request && openNode(node.id, node.name, node.request)}
    >
      <span className={`text-xs font-semibold w-12 ${methodColor(node.request?.method)}`}>
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
  const openBlank = useTabs((s) => s.openBlank);
  const patchDraft = useTabs((s) => s.patchDraft);
  const { data: items = [] } = useQuery({
    queryKey: ['history', workspaceId],
    queryFn: () => listHistory(workspaceId),
    refetchInterval: 3000, // 简单轮询；发送成功后也会主动 invalidate
  });

  const replay = (item: HistoryItem) => {
    openBlank();
    // openBlank 同步建 tab；取最新 active tab 打入快照
    const { activeId } = useTabs.getState();
    if (activeId) patchDraft(activeId, item.requestSnap);
  };

  if (items.length === 0) {
    return <p className="text-gray-400 text-center py-6 text-xs">暂无历史记录</p>;
  }
  return (
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
