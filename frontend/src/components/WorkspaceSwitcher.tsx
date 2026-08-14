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
import Dropdown from './Dropdown';
import QueryErrorState from './QueryErrorState';

interface Props {
  activeId: string;
  onSwitch(id: string): void;
}

export default function WorkspaceSwitcher({ activeId, onSwitch }: Props) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const workspacesQuery = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
  });
  const workspaces = workspacesQuery.data ?? [];
  const invalidate = () => qc.invalidateQueries({ queryKey: ['workspaces'] });
  const active = workspaces.find((w) => w.id === activeId);
  const [busy, setBusy] = useState(false);
  const removeSession = useTabs((state) => state.removeSession);

  const doCreate = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const name = await dialog.prompt(formatMessage('新工作区名称：'), {
        defaultValue: formatMessage('工作区 {index}', { index: workspaces.length + 1 }),
      });
      if (!name) return;
      const w = await createWorkspace(name);
      invalidate();
      onSwitch(w.id);
    } catch (e) {
      void dialog.alert(
        formatMessage('创建工作区失败: {detail}', { detail: toAppError(e).detail }),
        { title: formatMessage('创建工作区失败') },
      );
    } finally {
      setBusy(false);
    }
  };

  const doRename = async () => {
    if (!active || busy) return;
    setBusy(true);
    try {
      const name = await dialog.prompt(formatMessage('重命名工作区：'), { defaultValue: active.name });
      if (!name || name === active.name) return;
      await renameWorkspace(active.id, name);
      invalidate();
    } catch (e) {
      void dialog.alert(formatMessage('重命名失败: {detail}', { detail: toAppError(e).detail }), {
        title: formatMessage('重命名失败'),
      });
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    if (!active || busy) return;
    setBusy(true);
    try {
      if (
        !(await dialog.confirm(
          formatMessage('删除工作区「{name}」及其全部集合、环境与历史？此操作不可恢复。', {
            name: active.name,
          }),
        ))
      ) return;
      await deleteWorkspace(active.id);
      removeSession(active.id);
      invalidate();
      const rest = workspaces.filter((w) => w.id !== active.id);
      if (rest[0]) onSwitch(rest[0].id);
    } catch (e) {
      void dialog.alert(
        formatMessage('删除工作区失败: {detail}', { detail: toAppError(e).detail }),
        { title: formatMessage('删除工作区失败') },
      );
    } finally {
      setBusy(false);
    }
  };

  if (workspacesQuery.isError && workspaces.length === 0) {
    return (
      <div className="ml-3">
        <QueryErrorState
          message={formatMessage('工作区加载失败')}
          detail={toAppError(workspacesQuery.error).detail}
          onRetry={() => void workspacesQuery.refetch()}
        />
      </div>
    );
  }

  return (
    <div className="flex items-center gap-1 ml-3">
      <Dropdown
        value={activeId}
        options={workspaces.map((w) => ({ value: w.id, label: w.name }))}
        onChange={onSwitch}
        title={formatMessage('工作区')}
        placeholder={workspacesQuery.isPending ? formatMessage('加载中…') : ''}
        disabled={busy || workspacesQuery.isPending}
      />
      <button
        className="text-xs text-gray-500 hover:text-gray-800 px-1"
        title={formatMessage('新建工作区')}
        onClick={doCreate}
        disabled={busy || workspacesQuery.isPending}
      >
        +
      </button>
      <button
        className="text-xs text-gray-500 hover:text-gray-800 px-1"
        title={formatMessage('重命名')}
        onClick={doRename}
        disabled={busy || workspacesQuery.isPending}
      >
        ✎
      </button>
      {workspaces.length > 1 && (
        <button
          className="text-xs text-gray-400 hover:text-red-500 px-1"
          title={formatMessage('删除工作区')}
          onClick={doDelete}
          disabled={busy || workspacesQuery.isPending}
        >
          ×
        </button>
      )}
    </div>
  );
}
