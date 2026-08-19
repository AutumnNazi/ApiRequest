// 环境切换器（顶栏）+ 环境管理弹窗
import { useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listEnvironments,
  upsertEnvironment,
  deleteEnvironment,
  setActiveEnvironment,
  toAppError,
  type Environment,
  type Variable,
} from '../ipc';
import { useStableRowIds } from '../hooks/useStableRowIds';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';
import Dropdown from './Dropdown';
import QueryErrorState from './QueryErrorState';

interface Props {
  workspaceId: string;
  // Ctrl/Cmd+E 的展开信号，由 App 的快捷键处理递增
  openSignal?: number;
}

export default function EnvSwitcher({ workspaceId, openSignal }: Props) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const [managing, setManaging] = useState(false);
  const envQuery = useQuery({
    queryKey: ['envs', workspaceId],
    queryFn: () => listEnvironments(workspaceId),
  });
  const envs = envQuery.data ?? [];
  const active = envs.find((e) => e.isActive);

  const activate = useMutation({
    mutationFn: (envId: string) => setActiveEnvironment(workspaceId, envId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['envs', workspaceId] }),
    // 切换失败时：select.value 仍然绑 envs.isActive，不主动回滚也能保持 UI 一致
    // （mutation 失败不会改变后端 isActive），此处仅弹错给用户反馈
    onError: (e) =>
      void dialog.alert(
        formatMessage('切换环境失败: {detail}', { detail: toAppError(e).detail }),
        { title: formatMessage('切换环境失败') },
      ),
  });
  const unavailable = envQuery.isPending || activate.isPending;

  if (envQuery.isError && envs.length === 0) {
    return (
      <div className="ml-auto">
        <QueryErrorState
          message={formatMessage('环境加载失败')}
          detail={toAppError(envQuery.error).detail}
          onRetry={() => void envQuery.refetch()}
        />
      </div>
    );
  }

  return (
    <div className="ml-auto flex items-center gap-2">
      <Dropdown
        value={active?.id ?? ''}
        options={[
          { value: '', label: envQuery.isPending ? formatMessage('加载中…') : formatMessage('无环境') },
          ...envs.map((e) => ({ value: e.id, label: e.name })),
        ]}
        onChange={(v) => activate.mutate(v)}
        title={formatMessage('切换环境 (Ctrl+E)')}
        disabled={unavailable}
        openSignal={openSignal}
      >
        <button
          className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1 ml-1 disabled:cursor-not-allowed disabled:opacity-50"
          onClick={() => setManaging(true)}
          disabled={unavailable}
        >
          {formatMessage('管理')}
        </button>
      </Dropdown>
      {managing && (
        <EnvManager workspaceId={workspaceId} envs={envs} onClose={() => setManaging(false)} />
      )}
    </div>
  );
}

// ── 环境管理弹窗 ──

function EnvManager({
  workspaceId,
  envs,
  onClose,
}: {
  workspaceId: string;
  envs: Environment[];
  onClose(): void;
}) {
  const dialog = useDialog();
  const qc = useQueryClient();
  const [selectedId, setSelectedId] = useState<string | null>(envs[0]?.id ?? null);
  const selected = envs.find((e) => e.id === selectedId) ?? null;
  const invalidate = () => qc.invalidateQueries({ queryKey: ['envs', workspaceId] });

  const create = useMutation({
    mutationFn: () =>
      upsertEnvironment({
        workspaceId,
        name: formatMessage('环境 {index}', { index: envs.length + 1 }),
        variables: [],
      } as unknown as Environment),
    onSuccess: (created) => {
      invalidate();
      setSelectedId(created.id);
    },
    onError: (cause) => void dialog.alert(
      formatMessage('创建环境失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('创建环境失败') },
    ),
  });

  const save = useMutation({
    mutationFn: (env: Environment) => upsertEnvironment(env),
    onSuccess: invalidate,
    onError: (e) =>
      void dialog.alert(
        formatMessage('保存环境失败: {detail}', { detail: toAppError(e).detail }),
        { title: formatMessage('保存环境失败') },
      ),
  });

  const del = useMutation({
    mutationFn: (envId: string) => deleteEnvironment(envId),
    onSuccess: () => {
      invalidate();
      setSelectedId(null);
    },
    onError: (cause) => void dialog.alert(
      formatMessage('删除环境失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('删除环境失败') },
    ),
  });

  return (
    <div
      className="fixed inset-0 bg-black/30 flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="env-manager-title"
        className="bg-white rounded-lg shadow-xl w-[720px] h-[480px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 id="env-manager-title" className="font-semibold text-sm">{formatMessage('环境管理')}</h2>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>
        <div className="flex-1 flex min-h-0">
          {/* 环境列表 */}
          <div className="w-48 border-r flex flex-col">
            <div className="flex-1 overflow-auto py-1">
              {envs.map((e) => (
                <div
                  key={e.id}
                  className={`px-3 py-1.5 text-sm cursor-pointer flex items-center ${
                    e.id === selectedId ? 'bg-blue-50 text-blue-700' : 'hover:bg-gray-50'
                  }`}
                  onClick={() => setSelectedId(e.id)}
                >
                  <span className="truncate flex-1"><Verbatim value={e.name} /></span>
                  {e.isActive && <span className="w-1.5 h-1.5 rounded-full bg-green-500" />}
                </div>
              ))}
            </div>
            <button
              className="m-2 border border-dashed rounded py-1 text-xs text-gray-500 hover:text-gray-800 disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() => create.mutate()}
              disabled={create.isPending}
            >
              {create.isPending ? formatMessage('创建中…') : formatMessage('+ 新建环境')}
            </button>
          </div>
          {/* 变量编辑 */}
          {selected ? (
            <EnvEditor
              key={selected.id}
              env={selected}
              onSave={(env) => save.mutate(env)}
              onDelete={() => {
                void dialog.confirm(formatMessage('删除环境「{name}」？', { name: selected.name })).then((ok) => {
                  if (ok) del.mutate(selected.id);
                });
              }}
              saving={save.isPending}
              deleting={del.isPending}
            />
          ) : (
            <div className="flex-1 flex items-center justify-center text-gray-400 text-sm">
              {formatMessage('选择或新建一个环境')}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function EnvEditor({
  env,
  onSave,
  onDelete,
  saving,
  deleting,
}: {
  env: Environment;
  onSave(env: Environment): void;
  onDelete(): void;
  saving: boolean;
  deleting: boolean;
}) {
  const [name, setName] = useState(env.name);
  const [vars, setVars] = useState<Variable[]>(env.variables ?? []);
  const rows = [...vars, { key: '', value: '', type: 'default', enabled: true } as Variable];
  const { rowIds, promoteGhostRow, removeRow } = useStableRowIds(rows.length);

  const update = (idx: number, patch: Partial<Variable>) => {
    const next = rows.map((r, i) => (i === idx ? { ...r, ...patch } : r));
    const nextVars = next.filter((r, i) => !(i === next.length - 1 && !r.key && !r.value));
    if (nextVars.length > vars.length) promoteGhostRow();
    setVars(nextVars);
  };

  const remove = (idx: number) => {
    removeRow(idx);
    setVars(vars.filter((_, i) => i !== idx));
  };

  return (
    <div className="flex-1 flex flex-col min-w-0">
      <div className="flex gap-2 p-3 border-b">
        <input
          className="border rounded px-2 py-1 text-sm flex-1"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button
          className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
          onClick={() => onSave({ ...env, name, variables: vars } as Environment)}
          disabled={saving || deleting}
        >
          {saving ? formatMessage('保存中…') : formatMessage('保存')}
        </button>
        <button
          className="border border-red-200 text-red-500 rounded px-3 py-1 text-sm hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50"
          onClick={onDelete}
          disabled={saving || deleting}
        >
          {deleting ? formatMessage('删除中…') : formatMessage('删除')}
        </button>
      </div>
      <div className="flex-1 overflow-auto p-3">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-gray-500 border-b">
              <th className="w-8 p-1"></th>
              <th className="p-1 font-normal">{formatMessage('变量名')}</th>
              <th className="p-1 font-normal">{formatMessage('值')}</th>
              <th className="w-16 p-1 font-normal">{formatMessage('密钥')}</th>
              <th className="w-8 p-1"></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => {
              const isGhost = i === rows.length - 1;
              return (
                <tr key={rowIds[i]} className="border-b border-gray-100">
                  <td className="p-1 text-center">
                    {!isGhost && (
                      <input
                        type="checkbox"
                        checked={r.enabled}
                        onChange={(e) => update(i, { enabled: e.target.checked })}
                      />
                    )}
                  </td>
                  <td className="p-1">
                    <input
                      className="w-full px-1 py-0.5 outline-none focus:bg-blue-50 rounded font-mono"
                      placeholder={formatMessage('变量名')}
                      value={r.key}
                      onChange={(e) => update(i, { key: e.target.value })}
                    />
                  </td>
                  <td className="p-1">
                    <input
                      className="w-full px-1 py-0.5 outline-none focus:bg-blue-50 rounded font-mono"
                      placeholder={formatMessage('值')}
                      type={r.type === 'secret' ? 'password' : 'text'}
                      value={r.value}
                      onChange={(e) => update(i, { value: e.target.value })}
                    />
                  </td>
                  <td className="p-1 text-center">
                    {!isGhost && (
                      <input
                        type="checkbox"
                        checked={r.type === 'secret'}
                        onChange={(e) => update(i, { type: e.target.checked ? 'secret' : 'default' })}
                        title={formatMessage('密钥变量（掩码显示）')}
                      />
                    )}
                  </td>
                  <td className="p-1 text-center">
                    {!isGhost && (
                      <button
                        className="text-gray-400 hover:text-red-500"
                        onClick={() => remove(i)}
                      >
                        ×
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
