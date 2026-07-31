// Cookie 管理弹窗
import { useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import { listCookies, deleteCookie, clearCookies, upsertCookie, toAppError, type Cookie } from '../ipc';

interface Props {
  onClose(): void;
}

export default function CookieManager({ onClose }: Props) {
  const qc = useQueryClient();
  const { data: cookies = [] } = useQuery({ queryKey: ['cookies'], queryFn: () => listCookies() });
  const invalidate = () => qc.invalidateQueries({ queryKey: ['cookies'] });
  const importRef = useRef<HTMLInputElement>(null);

  const exportCookies = () => {
    const blob = new Blob([JSON.stringify(cookies, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'apirequest-cookies.json';
    link.click();
    URL.revokeObjectURL(url);
  };

  const importCookies = async (file?: File) => {
    if (!file) return;
    try {
      const parsed: unknown = JSON.parse(await file.text());
      const cookiesToImport = normalizeImportedCookies(parsed);
      // 先完整校验数组，再开始写入，避免后续坏项导致前半批静默落库。
      for (const cookie of cookiesToImport) {
        await upsertCookie(cookie);
      }
      invalidate();
    } catch (error) {
      alert('导入 Cookie 失败: ' + toAppError(error).detail);
    } finally {
      if (importRef.current) importRef.current.value = '';
    }
  };

  const del = useMutation({
    mutationFn: (c: { domain: string; path: string; name: string }) =>
      deleteCookie(c.domain, c.path, c.name),
    onSuccess: invalidate,
  });
  const clearAll = useMutation({
    mutationFn: () => clearCookies(),
    onSuccess: invalidate,
  });

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[680px] h-[440px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">Cookie 管理</h2>
          <span className="ml-2 text-xs text-gray-400">{cookies.length} 条</span>
          <input
            ref={importRef}
            className="hidden"
            type="file"
            accept="application/json,.json"
            onChange={(e) => importCookies(e.target.files?.[0])}
          />
          <button
            className="ml-auto text-xs border rounded px-2 py-1 hover:bg-gray-50"
            onClick={() => importRef.current?.click()}
          >
            导入
          </button>
          <button
            className="ml-2 text-xs border rounded px-2 py-1 hover:bg-gray-50"
            onClick={exportCookies}
            disabled={cookies.length === 0}
          >
            导出
          </button>
          <button
            className="ml-2 text-xs border border-red-200 text-red-500 rounded px-2 py-1 hover:bg-red-50"
            onClick={() => {
              if (confirm('清空全部 Cookie？')) clearAll.mutate();
            }}
          >
            全部清空
          </button>
          <button className="ml-2 text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>
        <div className="flex-1 overflow-auto">
          {cookies.length === 0 ? (
            <div className="h-full flex items-center justify-center text-gray-400 text-sm">
              暂无 Cookie；响应中的 Set-Cookie 会自动存入
            </div>
          ) : (
            <CookieTable cookies={cookies} onDelete={(c) => del.mutate(c)} />
          )}
        </div>
      </div>
    </div>
  );
}

// 按 domain 分组、可折叠；过期时间显示绝对 + 相对
function CookieTable({
  cookies,
  onDelete,
}: {
  cookies: Cookie[];
  onDelete(c: { domain: string; path: string; name: string }): void;
}) {
  // 按 domain 分组
  const groups = useMemo(() => {
    const m = new Map<string, typeof cookies>();
    for (const c of cookies) {
      const k = c.domain || '(无域)';
      const arr = m.get(k) ?? [];
      arr.push(c);
      m.set(k, arr);
    }
    return [...m.entries()];
  }, [cookies]);

  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggle = (d: string) =>
    setCollapsed((p) => {
      const n = new Set(p);
      n.has(d) ? n.delete(d) : n.add(d);
      return n;
    });

  return (
    <div className="text-xs">
      {groups.map(([domain, items]) => (
        <div key={domain}>
          <div
            className="sticky top-0 z-10 flex items-center gap-1 px-2 py-1.5 bg-gray-100 hover:bg-gray-200 cursor-pointer select-none border-b border-gray-200"
            onClick={() => toggle(domain)}
          >
            <span className="text-gray-500">{collapsed.has(domain) ? '▶' : '▼'}</span>
            <span className="font-mono font-medium text-gray-700">{domain}</span>
            <span className="text-gray-400">({items.length})</span>
          </div>
          {!collapsed.has(domain) && (
            <table className="w-full">
              <tbody>
                {items.map((c) => {
                  const exp = c.expires
                    ? new Date(c.expires)
                    : null;
                  const expired = exp && exp.getTime() < Date.now();
                  return (
                    <tr key={cookieKey(c)} className="border-b border-gray-100 hover:bg-gray-50">
                      <td className="p-2 font-mono">{c.path || '/'}</td>
                      <td className="p-2 font-mono font-medium">{c.name}</td>
                      <td className="p-2 font-mono max-w-40 truncate" title={c.value}>
                        {c.value}
                      </td>
                      <td className={`p-2 ${expired ? 'text-red-500' : 'text-gray-400'}`}>
                        {exp
                          ? `${exp.toLocaleString()}${
                              expired
                                ? '（已过期）'
                                : exp.getTime() - Date.now() < 86400000
                                  ? `（剩 ${Math.max(0, Math.round((exp.getTime() - Date.now()) / 3600000))} 小时）`
                                  : `（剩 ${Math.round((exp.getTime() - Date.now()) / 86400000)} 天）`
                            }`
                          : '会话'}
                      </td>
                      <td className="p-2">
                        <button
                          className="text-gray-400 hover:text-red-500"
                          onClick={() =>
                            onDelete({ domain: c.domain ?? '', path: c.path || '/', name: c.name })
                          }
                        >
                          ×
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      ))}
    </div>
  );
}

function cookieKey(cookie: Cookie): string {
  return [cookie.domain, cookie.path || '/', cookie.name].join('|');
}

function normalizeImportedCookies(parsed: unknown): Cookie[] {
  if (!Array.isArray(parsed)) throw new Error('文件根节点必须是 Cookie 数组');
  return parsed.map((value, index) => {
    const label = '第 ' + (index + 1) + ' 个 Cookie';
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      throw new Error(label + ' 必须是对象');
    }
    const raw = value as Record<string, unknown>;
    if (typeof raw.name !== 'string' || !raw.name.trim()) {
      throw new Error(label + ' 的 name 必须是非空字符串');
    }
    if (typeof raw.domain !== 'string' || !raw.domain.trim()) {
      throw new Error(label + ' 的 domain 必须是非空字符串');
    }
    if (raw.value !== undefined && typeof raw.value !== 'string') {
      throw new Error(label + ' 的 value 必须是字符串');
    }
    if (raw.path !== undefined && typeof raw.path !== 'string') {
      throw new Error(label + ' 的 path 必须是字符串');
    }
    if (raw.expires !== undefined &&
        (typeof raw.expires !== 'number' || !Number.isFinite(raw.expires))) {
      throw new Error(label + ' 的 expires 必须是有限数字');
    }
    if (raw.httpOnly !== undefined && typeof raw.httpOnly !== 'boolean') {
      throw new Error(label + ' 的 httpOnly 必须是布尔值');
    }
    if (raw.secure !== undefined && typeof raw.secure !== 'boolean') {
      throw new Error(label + ' 的 secure 必须是布尔值');
    }
    return {
      name: raw.name.trim(),
      value: (raw.value as string | undefined) ?? '',
      domain: raw.domain.trim(),
      path: (raw.path as string | undefined) || '/',
      expires: (raw.expires as number | undefined) ?? 0,
      httpOnly: (raw.httpOnly as boolean | undefined) ?? false,
      secure: (raw.secure as boolean | undefined) ?? false,
    };
  });
}
