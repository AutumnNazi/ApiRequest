// 变量解析预览：扫描文本中的 {{var}}，对照激活环境+全局变量给出解析提示
import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { listEnvironments, getGlobalVariables } from '../ipc';

/** 当前生效变量表（激活环境 > 全局；不含集合级——需节点上下文，此处近似） */
export function useActiveVariables(workspaceId: string): Map<string, string> {
  const { data: envs = [] } = useQuery({
    queryKey: ['envs', workspaceId],
    queryFn: () => listEnvironments(workspaceId),
  });
  const { data: globals = [] } = useQuery({
    queryKey: ['globals', workspaceId],
    queryFn: () => getGlobalVariables(workspaceId),
  });

  return useMemo(() => {
    const map = new Map<string, string>();
    for (const v of globals) {
      if (v.enabled && v.key) map.set(v.key, v.value);
    }
    const active = envs.find((e) => e.isActive);
    for (const v of active?.variables ?? []) {
      if (v.enabled && v.key) map.set(v.key, v.value);
    }
    return map;
  }, [envs, globals]);
}

const VAR_RE = /\{\{\s*([^}]+?)\s*\}\}/g;

interface Props {
  text: string;
  vars: Map<string, string>;
}

/** URL/文本中变量引用的解析结果提示条；无变量时不渲染 */
export default function VarPreview({ text, vars }: Props) {
  const refs = useMemo(() => {
    const seen = new Set<string>();
    const out: { name: string; value?: string; dynamic: boolean }[] = [];
    for (const m of text.matchAll(VAR_RE)) {
      const name = m[1];
      if (seen.has(name)) continue;
      seen.add(name);
      if (name.startsWith('$')) {
        out.push({ name, dynamic: true });
      } else {
        out.push({ name, value: vars.get(name), dynamic: false });
      }
    }
    return out;
  }, [text, vars]);

  if (refs.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-x-3 gap-y-0.5 px-3 py-1 text-xs border-b bg-gray-50">
      {refs.map((r) => (
        <span key={r.name} className="font-mono">
          {r.dynamic ? (
            <span className="text-purple-600" title="动态变量，发送时生成">
              {'{{'}{r.name}{'}}'}
            </span>
          ) : r.value !== undefined ? (
            <>
              <span className="text-green-700">{'{{'}{r.name}{'}}'}</span>
              <span className="text-gray-400"> = </span>
              <span className="text-gray-600" title={r.value}>
                {r.value.length > 30 ? r.value.slice(0, 30) + '…' : r.value || '(空)'}
              </span>
            </>
          ) : (
            <span
              className="text-red-500 underline decoration-wavy decoration-red-300"
              title="未在激活环境/全局变量中定义（集合级变量发送时仍会解析）"
            >
              {'{{'}{r.name}{'}}'}
            </span>
          )}
        </span>
      ))}
    </div>
  );
}
