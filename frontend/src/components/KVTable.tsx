// 键值表格：Header/Query 共用。末行自动新增空行（docs/frontend.md §4）。
import { useMemo, useState } from 'react';
import type { KV } from '../ipc';
import { useStableRowIds } from '../hooks/useStableRowIds';
import { formatMessage } from '../i18n/locale';

// 常用 HTTP 请求头，用于 Headers Key 自动补全
export const COMMON_HEADERS = [
  'Accept',
  'Accept-Charset',
  'Accept-Encoding',
  'Accept-Language',
  'Authorization',
  'Cache-Control',
  'Connection',
  'Content-Disposition',
  'Content-Encoding',
  'Content-Length',
  'Content-Type',
  'Cookie',
  'Date',
  'DNT',
  'ETag',
  'Expect',
  'Forwarded',
  'From',
  'Host',
  'If-Match',
  'If-Modified-Since',
  'If-None-Match',
  'If-Range',
  'If-Unmodified-Since',
  'Max-Forwards',
  'Origin',
  'Pragma',
  'Proxy-Authorization',
  'Range',
  'Referer',
  'Referrer-Policy',
  'Retry-After',
  'Server',
  'Set-Cookie',
  'Strict-Transport-Security',
  'Transfer-Encoding',
  'Upgrade-Insecure-Requests',
  'User-Agent',
  'Vary',
  'Via',
  'WWW-Authenticate',
  'X-Api-Key',
  'X-Auth-Token',
  'X-Content-Type-Options',
  'X-Forwarded-For',
  'X-Forwarded-Host',
  'X-Forwarded-Proto',
  'X-Frame-Options',
  'X-Requested-With',
  'X-XSS-Protection',
];

interface Props {
  items: KV[];
  onChange(items: KV[]): void;
  keySuggestions?: string[];
}

export default function KVTable({ items, onChange, keySuggestions }: Props) {
  // 展示时末尾始终跟一个空行，编辑空行即新增
  const rows = [...items, { key: '', value: '', enabled: true } as KV];
  const { rowIds, promoteGhostRow, removeRow } = useStableRowIds(rows.length);
  // Key 自动补全：记录当前编辑行与输入值
  const [autoIdx, setAutoIdx] = useState<number | null>(null);
  const [autoQuery, setAutoQuery] = useState('');
  // 重复 Key 检测：同一组内同名 Key 是常见踩坑点（后者覆盖前者），红色高亮提示。
  const dupKeys = useMemo(() => {
    const seen = new Set<string>();
    const dups = new Set<string>();
    for (const it of items) {
      const k = it.key?.trim().toLowerCase();
      if (!k) continue;
      if (seen.has(k)) dups.add(k);
      seen.add(k);
    }
    return dups;
  }, [items]);

  const filtered = keySuggestions && autoIdx !== null
    ? keySuggestions
        .filter((s) => s.toLowerCase().includes(autoQuery.toLowerCase()))
        .filter((s) => s.toLowerCase() !== autoQuery.toLowerCase())
        .slice(0, 8)
    : [];

  const update = (idx: number, patch: Partial<KV>) => {
    const next = rows.map((r, i) => (i === idx ? { ...r, ...patch } : r));
    // 去掉末尾完全为空的行再回传
    const nextItems = next.filter((r, i) => !(i === next.length - 1 && !r.key && !r.value));
    if (nextItems.length > items.length) {
      // 原 ghost identity 留给刚创建的行，新 identity 属于新的 ghost 行。
      promoteGhostRow();
    }
    onChange(nextItems);
  };

  const remove = (idx: number) => {
    removeRow(idx);
    onChange(items.filter((_, i) => i !== idx));
  };

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="text-left text-gray-500 border-b">
          <th className="w-8 p-1"></th>
          <th className="p-1 font-normal">Key</th>
          <th className="p-1 font-normal">Value</th>
          <th className="w-8 p-1"></th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => {
          const isGhost = i === rows.length - 1;
          const isDup = !isGhost && r.key && dupKeys.has(r.key.trim().toLowerCase());
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
                <div className="relative">
                  <input
                    className={`w-full px-1 py-0.5 outline-none focus:bg-blue-50 ${
                      isDup ? 'bg-red-50 text-red-700' : ''
                    }`}
                    placeholder="Key"
                    title={isDup ? formatMessage('重复的 Key，后者会覆盖前者') : undefined}
                    value={r.key}
                    onChange={(e) => {
                      update(i, { key: e.target.value });
                      if (keySuggestions) {
                        setAutoIdx(i);
                        setAutoQuery(e.target.value);
                      }
                    }}
                    onFocus={() => {
                      if (keySuggestions && r.key) {
                        setAutoIdx(i);
                        setAutoQuery(r.key);
                      }
                    }}
                    onBlur={() => setTimeout(() => setAutoIdx(null), 150)}
                  />
                  {autoIdx === i && filtered.length > 0 && (
                    <div className="absolute top-full left-0 z-20 max-h-[200px] min-w-[200px] overflow-auto rounded border bg-white py-1 text-xs shadow-lg">
                      {filtered.map((s) => (
                        <button
                          key={s}
                          type="button"
                          className="block w-full px-2 py-1 text-left text-gray-700 hover:bg-blue-50"
                          onMouseDown={(e) => {
                            e.preventDefault();
                            update(i, { key: s });
                            setAutoIdx(null);
                          }}
                        >
                          {s}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              </td>
              <td className="p-1">
                <input
                  className="w-full px-1 py-0.5 outline-none focus:bg-blue-50"
                  placeholder="Value"
                  value={r.value}
                  onChange={(e) => update(i, { value: e.target.value })}
                />
              </td>
              <td className="p-1 text-center">
                {!isGhost && (
                  <button
                    className="text-gray-400 hover:text-red-500"
                    onClick={() => remove(i)}
                    title={formatMessage('删除')}
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
  );
}
