// 环境切换器（顶栏）+ 环境管理弹窗
import { useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import {
  listEnvironments,
  upsertEnvironment,
  deleteEnvironment,
  setActiveEnvironment,
  type Environment,
  type Variable,
} from '../ipc';

interface Props {
  workspaceId: string;
}

export default function EnvSwitcher({ workspaceId }: Props) {
  const qc = useQueryClient();
  const [managing, setManaging] = useState(false);
  const { data: envs = [] } = useQuery({
    queryKey: ['envs', workspaceId],
    queryFn: () => listEnvironments(workspaceId),
  });
  const active = envs.find((e) => e.isActive);

  const activate = useMutation({
    mutationFn: (envId: string) => setActiveEnvironment(workspaceId, envId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['envs', workspaceId] }),
  });

  return (
    <div className="ml-auto flex items-center gap-2">
      <select
        className="border rounded px-2 py-1 text-xs bg-white"
        value={active?.id ?? ''}
        onChange={(e) => activate.mutate(e.target.value)}
        title="切换环境 (Ctrl+E)"
      >
        <option value="">No Environment</option>
        {envs.map((e) => (
          <option key={e.id} value={e.id}>
            {e.name}
          </option>
        ))}
      </select>
      <button
        className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
        onClick={() => setManaging(true)}
      >
        管理
      </button>
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
  const qc = useQueryClient();
  const [selectedId, setSelectedId] = useState<string | null>(envs[0]?.id ?? null);
  const selected = envs.find((e) => e.id === selectedId) ?? null;
  const invalidate = () => qc.invalidateQueries({ queryKey: ['envs', workspaceId] });

  const create = useMutation({
    mutationFn: () =>
      upsertEnvironment({
        workspaceId,
        name: `环境 ${envs.length + 1}`,
        variables: [],
      } as unknown as Environment),
    onSuccess: (created) => {
      invalidate();
      setSelectedId(created.id);
    },
  });

  const save = useMutation({
    mutationFn: (env: Environment) => upsertEnvironment(env),
    onSuccess: invalidate,
  });

  const del = useMutation({
    mutationFn: (envId: string) => deleteEnvironment(envId),
    onSuccess: () => {
      invalidate();
      setSelectedId(null);
    },
  });

  return (
    <div
      className="fixed inset-0 bg-black/30 flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-lg shadow-xl w-[720px] h-[480px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">环境管理</h2>
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
                  <span className="truncate flex-1">{e.name}</span>
                  {e.isActive && <span className="w-1.5 h-1.5 rounded-full bg-green-500" />}
                </div>
              ))}
            </div>
            <button
              className="m-2 border border-dashed rounded py-1 text-xs text-gray-500 hover:text-gray-800"
              onClick={() => create.mutate()}
            >
              + 新建环境
            </button>
          </div>
          {/* 变量编辑 */}
          {selected ? (
            <EnvEditor
              key={selected.id}
              env={selected}
              onSave={(env) => save.mutate(env)}
              onDelete={() => {
                if (confirm(`删除环境「${selected.name}」？`)) del.mutate(selected.id);
              }}
            />
          ) : (
            <div className="flex-1 flex items-center justify-center text-gray-400 text-sm">
              选择或新建一个环境
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
}: {
  env: Environment;
  onSave(env: Environment): void;
  onDelete(): void;
}) {
  const [name, setName] = useState(env.name);
  const [vars, setVars] = useState<Variable[]>(env.variables ?? []);
  const rows = [...vars, { key: '', value: '', type: 'default', enabled: true } as Variable];

  const update = (idx: number, patch: Partial<Variable>) => {
    const next = rows.map((r, i) => (i === idx ? { ...r, ...patch } : r));
    setVars(next.filter((r, i) => !(i === next.length - 1 && !r.key && !r.value)));
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
          className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700"
          onClick={() => onSave({ ...env, name, variables: vars } as Environment)}
        >
          保存
        </button>
        <button
          className="border border-red-200 text-red-500 rounded px-3 py-1 text-sm hover:bg-red-50"
          onClick={onDelete}
        >
          删除
        </button>
      </div>
      <div className="flex-1 overflow-auto p-3">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-gray-500 border-b">
              <th className="w-8 p-1"></th>
              <th className="p-1 font-normal">变量名</th>
              <th className="p-1 font-normal">值</th>
              <th className="w-16 p-1 font-normal">密钥</th>
              <th className="w-8 p-1"></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => {
              const isGhost = i === rows.length - 1;
              return (
                <tr key={i} className="border-b border-gray-100">
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
                      placeholder="变量名"
                      value={r.key}
                      onChange={(e) => update(i, { key: e.target.value })}
                    />
                  </td>
                  <td className="p-1">
                    <input
                      className="w-full px-1 py-0.5 outline-none focus:bg-blue-50 rounded font-mono"
                      placeholder="值"
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
                        title="密钥变量（掩码显示）"
                      />
                    )}
                  </td>
                  <td className="p-1 text-center">
                    {!isGhost && (
                      <button
                        className="text-gray-400 hover:text-red-500"
                        onClick={() => setVars(vars.filter((_, vi) => vi !== i))}
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
