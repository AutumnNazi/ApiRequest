// Mock Server 面板：启停 + 地址 + 请求日志
import { useEffect, useState } from 'react';
import {
  startMockServer,
  stopMockServer,
  runningMocks,
  onMockLog,
  toAppError,
  type MockLogEntry,
} from '../ipc';

interface Props {
  collectionId: string;
  collectionName: string;
  onClose(): void;
}

export default function MockPanel({ collectionId, collectionName, onClose }: Props) {
  const [addr, setAddr] = useState('');
  const [error, setError] = useState('');
  const [logs, setLogs] = useState<MockLogEntry[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    runningMocks().then((m) => setAddr(m[collectionId] ?? ''));
  }, [collectionId]);

  useEffect(() => onMockLog((entry) => {
    if (entry.collectionId === collectionId) {
      setLogs((prev) => [entry, ...prev].slice(0, 200));
    }
  }), [collectionId]);

  const toggle = async () => {
    setError('');
    setBusy(true);
    try {
      if (addr) {
        await stopMockServer(collectionId);
        setAddr('');
      } else {
        const status = await startMockServer(collectionId);
        setAddr(status.addr);
      }
    } catch (e) {
      setError(toAppError(e).detail);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[640px] h-[460px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">Mock · {collectionName}</h2>
          <span
            className={`w-2 h-2 rounded-full ${addr ? 'bg-green-500' : 'bg-gray-300'}`}
            title={addr ? '运行中' : '已停止'}
          />
          <button
            className={`ml-auto text-sm rounded px-4 py-1 ${
              addr
                ? 'border border-red-200 text-red-500 hover:bg-red-50'
                : 'bg-blue-600 text-white hover:bg-blue-700'
            } disabled:opacity-50`}
            disabled={busy}
            onClick={toggle}
          >
            {addr ? '停止' : '启动'}
          </button>
          <button className="text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>

        {addr && (
          <div className="flex items-center gap-2 px-4 py-2 bg-green-50 border-b text-sm">
            <span className="font-mono text-green-800">{addr}</span>
            <button
              className="text-xs text-green-700 hover:underline"
              onClick={() => navigator.clipboard.writeText(addr)}
            >
              复制
            </button>
          </div>
        )}
        {error && (
          <div className="px-4 py-2 bg-red-50 border-b text-xs text-red-600">{error}</div>
        )}

        <div className="flex-1 overflow-auto">
          {logs.length === 0 ? (
            <div className="h-full flex items-center justify-center text-gray-400 text-sm px-8 text-center">
              {addr
                ? '等待请求…向上方地址发请求即返回对应示例'
                : '启动后，此集合中带"示例"的请求会按路径/方法提供 mock 响应'}
            </div>
          ) : (
            <table className="w-full text-xs">
              <tbody>
                {logs.map((l, i) => (
                  <tr key={i} className="border-b border-gray-100">
                    <td className="p-2 font-semibold w-16">{l.method}</td>
                    <td className="p-2 font-mono">{l.path}</td>
                    <td className="p-2 text-gray-400">{l.matched || '—'}</td>
                    <td
                      className={`p-2 w-12 text-right ${
                        l.status < 400 ? 'text-green-600' : 'text-red-500'
                      }`}
                    >
                      {l.status}
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
