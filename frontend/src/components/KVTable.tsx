// 键值表格：Header/Query 共用。末行自动新增空行（docs/frontend.md §4）。
import type { KV } from '../ipc';

interface Props {
  items: KV[];
  onChange(items: KV[]): void;
}

export default function KVTable({ items, onChange }: Props) {
  // 展示时末尾始终跟一个空行，编辑空行即新增
  const rows = [...items, { key: '', value: '', enabled: true } as KV];

  const update = (idx: number, patch: Partial<KV>) => {
    const next = rows.map((r, i) => (i === idx ? { ...r, ...patch } : r));
    // 去掉末尾完全为空的行再回传
    onChange(next.filter((r, i) => !(i === next.length - 1 && !r.key && !r.value)));
  };

  const remove = (idx: number) => {
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
                  className="w-full px-1 py-0.5 outline-none focus:bg-blue-50 rounded"
                  placeholder="Key"
                  value={r.key}
                  onChange={(e) => update(i, { key: e.target.value })}
                />
              </td>
              <td className="p-1">
                <input
                  className="w-full px-1 py-0.5 outline-none focus:bg-blue-50 rounded"
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
                    title="删除"
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
