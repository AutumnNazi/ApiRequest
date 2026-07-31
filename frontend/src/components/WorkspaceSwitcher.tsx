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
import { useTabs } from '../stores/tabs';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';

interface Props {
  activeId: string;
  onSwitch(id: string): void;
}

export default function WorkspaceSwitcher({ activeId, onSwitch }: Props) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const { data: workspaces = [] } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ['workspaces'] });
  const active = workspaces.find((w) => w.id === activeId);
  const [busy, setBusy] = useState(false);
  const removeSession = useTabs((state) => state.removeSession);

  const doCreate = async () => {
    const name = await dialog.prompt('新工作区名称：', {
      defaultValue: formatMessage('工作区 {index}', { index: workspaces.length + 1 }),
    });
    if (!name) return;
    setBusy(true);
    try {
      const w = await createWorkspace(name);
      invalidate();
      onSwitch(w.id);
    } catch (e) {
      void dialog.alert(
        formatMessage('创建工作区失败: {detail}', { detail: toAppError(e).detail }),
        { title: '创建工作区失败' },
      );
    } finally {
      setBusy(false);
    }
  };

  const doRename = async () => {
    if (!active) return;
    const name = await dialog.prompt('重命名工作区：', { defaultValue: active.name });
    if (!name || name === active.name) return;
    try {
      await renameWorkspace(active.id, name);
      invalidate();
    } catch (e) {
      void dialog.alert(formatMessage('重命名失败: {detail}', { detail: toAppError(e).detail }), {
        title: '重命名失败',
      });
    }
  };

  const doDelete = async () => {
    if (!active) return;
    if (
      !(await dialog.confirm(
        formatMessage('删除工作区「{name}」及其全部集合、环境与历史？此操作不可恢复。', {
          name: active.name,
        }),
      ))
    ) return;
    try {
      await deleteWorkspace(active.id);
      removeSession(active.id);
      invalidate();
      const rest = workspaces.filter((w) => w.id !== active.id);
      if (rest[0]) onSwitch(rest[0].id);
    } catch (e) {
      void dialog.alert(
        formatMessage('删除工作区失败: {detail}', { detail: toAppError(e).detail }),
        { title: '删除工作区失败' },
      );
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
            <Verbatim value={w.name} />
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
