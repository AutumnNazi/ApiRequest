// 工作区切换器（顶栏）：下拉切换 + 新建/改名/删除
import { useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listWorkspaces,
  createWorkspace,
  renameWorkspace,
  deleteWorkspace,
  toAppError,
} from '../ipc';

interface Props {
  activeId: string;
  onSwitch(id: string): void;
}

export default function WorkspaceSwitcher({ activeId, onSwitch }: Props) {
  const qc = useQueryClient();
  const { data: workspaces = [] } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ['workspaces'] });
  const active = workspaces.find((w) => w.id === activeId);
  const [busy, setBusy] = useState(false);

  const doCreate = async () => {
    const name = prompt('新工作区名称：', `工作区 ${workspaces.length + 1}`);
    if (!name) return;
    setBusy(true);
    try {
      const w = await createWorkspace(name);
      invalidate();
      onSwitch(w.id);
    } catch (e) {
      alert(toAppError(e).detail);
    } finally {
      setBusy(false);
    }
  };

  const doRename = async () => {
    if (!active) return;
    const name = prompt('重命名工作区：', active.name);
    if (!name || name === active.name) return;
    try {
      await renameWorkspace(active.id, name);
      invalidate();
    } catch (e) {
      alert(toAppError(e).detail);
    }
  };

  const doDelete = async () => {
    if (!active) return;
    if (!confirm(`删除工作区「${active.name}」及其全部集合、环境与历史？此操作不可恢复。`)) return;
    try {
      await deleteWorkspace(active.id);
      invalidate();
      const rest = workspaces.filter((w) => w.id !== active.id);
      if (rest[0]) onSwitch(rest[0].id);
    } catch (e) {
      alert(toAppError(e).detail);
    }
  };

  return (
    <div className="flex items-center gap-1 ml-3">
      <select
        className="border rounded px-2 py-1 text-xs bg-white max-w-40"
        value={activeId}
        onChange={(e) => onSwitch(e.target.value)}
        disabled={busy}
      >
        {workspaces.map((w) => (
          <option key={w.id} value={w.id}>
            {w.name}
          </option>
        ))}
      </select>
      <button
        className="text-xs text-gray-500 hover:text-gray-800 px-1"
        title="新建工作区"
        onClick={doCreate}
      >
        +
      </button>
      <button
        className="text-xs text-gray-500 hover:text-gray-800 px-1"
        title="重命名"
        onClick={doRename}
      >
        ✎
      </button>
      {workspaces.length > 1 && (
        <button
          className="text-xs text-gray-400 hover:text-red-500 px-1"
          title="删除工作区"
          onClick={doDelete}
        >
          ×
        </button>
      )}
    </div>
  );
}
