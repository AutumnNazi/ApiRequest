// 响应查看器：状态行 + Body(Pretty/Raw)/Headers 页签 + 分阶段计时
import { useMemo, useState } from 'react';
import type { ResponseResult, AppError } from '../ipc';

interface Props {
  response?: ResponseResult;
  error?: AppError;
  sending: boolean;
}

const errorKindLabel: Record<string, string> = {
  network: '网络错误',
  tls: 'TLS 错误',
  validation: '请求无效',
  storage: '存储错误',
  script: '脚本错误',
  import: '导入错误',
  unknown: '错误',
};

export default function ResponseViewer({ response, error, sending }: Props) {
  const [pane, setPane] = useState<'body' | 'headers' | 'timing'>('body');
  const [raw, setRaw] = useState(false);

  const pretty = useMemo(() => {
    if (!response?.body?.text) return '';
    if (raw) return response.body.text;
    try {
      return JSON.stringify(JSON.parse(response.body.text), null, 2);
    } catch {
      return response.body.text;
    }
  }, [response, raw]);

  if (sending) {
    return <Center>发送中…</Center>;
  }
  if (error) {
    return (
      <div className="p-4">
        <div className="border border-red-200 bg-red-50 rounded p-3 text-sm">
          <div className="font-medium text-red-700 mb-1">
            {errorKindLabel[error.kind] ?? '错误'}
          </div>
          <div className="text-red-600 font-mono break-all">{error.detail}</div>
        </div>
      </div>
    );
  }
  if (!response) {
    return <Center>发送请求后在此查看响应</Center>;
  }

  const statusColor =
    response.status < 300 ? 'text-green-600' : response.status < 400 ? 'text-yellow-600' : 'text-red-600';

  return (
    <div className="flex flex-col h-full">
      {/* 状态行 */}
      <div className="flex items-center gap-4 px-3 py-2 border-b text-sm">
        <span className={`font-semibold ${statusColor}`}>
          {response.status} {response.statusText}
        </span>
        <span className="text-gray-500">{response.timing.totalMs.toFixed(0)} ms</span>
        <span className="text-gray-500">{formatSize(response.sizeBytes)}</span>
      </div>

      {/* 页签 */}
      <div className="flex gap-4 px-3 pt-2 border-b text-sm">
        {(
          [
            ['body', 'Body'],
            ['headers', `Headers (${response.headers.length})`],
            ['timing', '计时'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            className={`pb-2 border-b-2 -mb-px ${
              pane === key
                ? 'border-blue-600 text-blue-600'
                : 'border-transparent text-gray-600 hover:text-gray-900'
            }`}
            onClick={() => setPane(key)}
          >
            {label}
          </button>
        ))}
        {pane === 'body' && (
          <button
            className="ml-auto pb-2 text-xs text-gray-500 hover:text-gray-800"
            onClick={() => setRaw(!raw)}
          >
            {raw ? 'Pretty' : 'Raw'}
          </button>
        )}
      </div>

      <div className="flex-1 overflow-auto">
        {pane === 'body' && (
          <pre className="p-3 text-xs font-mono whitespace-pre-wrap break-all">{pretty}</pre>
        )}
        {pane === 'headers' && (
          <table className="w-full text-sm m-3">
            <tbody>
              {response.headers.map((h, i) => (
                <tr key={i} className="border-b border-gray-100">
                  <td className="p-1 pr-4 font-medium text-gray-700 whitespace-nowrap align-top">
                    {h.key}
                  </td>
                  <td className="p-1 font-mono text-xs break-all">{h.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {pane === 'timing' && <TimingBars t={response.timing} />}
      </div>
    </div>
  );
}

function TimingBars({ t }: { t: ResponseResult['timing'] }) {
  const stages: [string, number][] = [
    ['DNS', t.dnsMs],
    ['连接', t.connectMs],
    ['TLS', t.tlsMs],
    ['首字节 (TTFB)', t.ttfbMs],
    ['下载', t.downloadMs],
  ];
  const max = Math.max(t.totalMs, 1);
  return (
    <div className="p-4 space-y-2 text-sm max-w-xl">
      {stages.map(([name, ms]) => (
        <div key={name} className="flex items-center gap-2">
          <span className="w-28 text-gray-600">{name}</span>
          <div className="flex-1 bg-gray-100 rounded h-3">
            <div
              className="bg-blue-400 h-3 rounded"
              style={{ width: `${Math.min((ms / max) * 100, 100)}%` }}
            />
          </div>
          <span className="w-20 text-right text-gray-500">{ms.toFixed(1)} ms</span>
        </div>
      ))}
      <div className="flex items-center gap-2 pt-1 border-t">
        <span className="w-28 font-medium">总计</span>
        <span className="text-gray-700">{t.totalMs.toFixed(1)} ms</span>
      </div>
    </div>
  );
}

function Center({ children }: { children: React.ReactNode }) {
  return (
    <div className="h-full flex items-center justify-center text-gray-400 text-sm">{children}</div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}
