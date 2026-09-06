// 命令面板（Ctrl/Cmd+K）：请求跳转、工作区/环境切换、新建请求。
// 数据复用全局查询缓存（['nodes'] 与侧栏共享）；上下键选择、回车执行。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import ModalFrame from './ModalFrame';
import { useTabs } from '../stores/tabs';
import {
  getNode,
  listNodes,
  listWorkspaces,
  listEnvironments,
  setActiveEnvironment,
  type NodeSummary,
} from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';

interface CommandItem {
  key: string;
  label: string;
  hint: string;
  run(): void | Promise<void>;
}

const kindHint: Record<string, string> = {
  request: formatMessage('请求'),
  folder: formatMessage('文件夹'),
  collection: formatMessage('集合'),
};

export default function CommandPalette({
  workspaceId,
  onSwitchWorkspace,
  onClose,
}: {
  workspaceId: string;
  onSwitchWorkspace(id: string, name: string): void;
  onClose(): void;
}) {
  const [search, setSearch] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const openNode = useTabs((s) => s.openNode);
  const openBlank = useTabs((s) => s.openBlank);

  const nodesQuery = useQuery({
    queryKey: ['nodes', workspaceId],
    queryFn: () => listNodes(workspaceId),
  });
  const workspacesQuery = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
  });
  const envsQuery = useQuery({
    queryKey: ['envs', workspaceId],
    queryFn: () => listEnvironments(workspaceId),
  });

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const items = useMemo<CommandItem[]>(() => {
    const nodes = nodesQuery.data ?? [];
    const out: CommandItem[] = [];
    for (const n of nodes as NodeSummary[]) {
      if (n.kind !== 'request') continue;
      out.push({
        key: `node:${n.id}`,
        label: n.name,
        hint: kindHint[n.kind] ?? n.kind,
        run: async () => {
          const node = await getNode(n.workspaceId, n.id);
          if (node.request) openNode(node.workspaceId, node.id, node.name, node.request);
        },
      });
    }
    for (const w of workspacesQuery.data ?? []) {
      if (w.id === workspaceId) continue;
      out.push({
        key: `ws:${w.id}`,
        label: w.name,
        hint: formatMessage('切换工作区'),
        run: () => onSwitchWorkspace(w.id, w.name),
      });
    }
    for (const e of envsQuery.data ?? []) {
      out.push({
        key: `env:${e.id}`,
        label: e.name,
        hint: formatMessage('切换环境'),
        run: async () => {
          await setActiveEnvironment(workspaceId, e.id);
        },
      });
    }
    out.push({
      key: 'action:new-request',
      label: formatMessage('新建请求'),
      hint: formatMessage('动作'),
      run: () => {
        openBlank(workspaceId);
      },
    });
    return out;
  }, [nodesQuery.data, workspacesQuery.data, envsQuery.data, workspaceId, openNode, openBlank, onSwitchWorkspace]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return items;
    return items.filter((it) => it.label.toLowerCase().includes(q));
  }, [items, search]);

  useEffect(() => {
    setActive(0);
  }, [search]);

  const execute = (item: CommandItem | undefined) => {
    if (!item) return;
    void Promise.resolve(item.run()).finally(onClose);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive((i) => Math.min(filtered.length - 1, i + 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((i) => Math.max(0, i - 1));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      execute(filtered[active]);
    }
  };

  return (
    <ModalFrame
      className="w-[560px] max-w-[92vw] h-[380px]"
      onClose={onClose}
      titleId="command-palette-title"
    >
      <div className="flex flex-col h-full" role="dialog" aria-labelledby="command-palette-title" onKeyDown={onKeyDown}>
        <div className="px-4 py-3 border-b">
          <h2 id="command-palette-title" className="sr-only">{formatMessage('命令面板')}</h2>
          <input
            ref={inputRef}
            className="w-full border rounded px-3 py-2 text-sm outline-none focus:border-blue-400"
            placeholder={formatMessage('输入以筛选：请求 / 工作区 / 环境 / 动作…')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <div className="flex-1 overflow-auto text-sm">
          {filtered.length === 0 && (
            <p className="text-gray-400 text-center py-8 text-xs">{formatMessage('没有匹配的命令')}</p>
          )}
          {filtered.map((item, i) => (
            <button
              key={item.key}
              data-cmd-item
              className={`w-full text-left px-4 py-2 flex items-center gap-2 border-b border-gray-50 ${
                i === active ? 'bg-blue-50' : 'hover:bg-gray-50'
              }`}
              onMouseEnter={() => setActive(i)}
              onClick={() => execute(item)}
            >
              <span className="flex-1 min-w-0 truncate"><Verbatim value={item.label} /></span>
              <span className="text-xs text-gray-400 shrink-0"><Verbatim value={item.hint} /></span>
            </button>
          ))}
        </div>
      </div>
    </ModalFrame>
  );
}
