// Cookie 管理弹窗
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import { listCookies, deleteCookie, clearCookies } from '../ipc';

interface Props {
  onClose(): void;
}

export default function CookieManager({ onClose }: Props) {
  const qc = useQueryClient();
  const { data: cookies = [] } = useQuery({ queryKey: ['cookies'], queryFn: () => listCookies() });
  const invalidate = () => qc.invalidateQueries({ queryKey: ['cookies'] });

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
          <button
            className="ml-auto text-xs border border-red-200 text-red-500 rounded px-2 py-1 hover:bg-red-50"
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
            <table className="w-full text-xs">
              <thead className="sticky top-0 bg-gray-50">
                <tr className="text-left text-gray-500">
                  <th className="p-2 font-normal">域</th>
                  <th className="p-2 font-normal">路径</th>
                  <th className="p-2 font-normal">名称</th>
                  <th className="p-2 font-normal">值</th>
                  <th className="p-2 font-normal">过期</th>
                  <th className="p-2 w-8"></th>
                </tr>
              </thead>
              <tbody>
                {cookies.map((c, i) => (
                  <tr key={i} className="border-b border-gray-100 hover:bg-gray-50">
                    <td className="p-2 font-mono">{c.domain}</td>
                    <td className="p-2 font-mono">{c.path || '/'}</td>
                    <td className="p-2 font-mono font-medium">{c.name}</td>
                    <td className="p-2 font-mono max-w-40 truncate" title={c.value}>
                      {c.value}
                    </td>
                    <td className="p-2 text-gray-400">
                      {c.expires ? new Date(c.expires).toLocaleDateString() : '会话'}
                    </td>
                    <td className="p-2">
                      <button
                        className="text-gray-400 hover:text-red-500"
                        onClick={() =>
                          del.mutate({ domain: c.domain ?? '', path: c.path || '/', name: c.name })
                        }
                      >
                        ×
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}
