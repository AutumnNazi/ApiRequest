// 响应查看器：状态行 + Body(Pretty/Raw)/Headers 页签 + 分阶段计时
import { useEffect, useMemo, useState } from 'react';
import { upsertExample, getResponseBlob, toAppError, type ResponseResult, type AppError, type Example } from '../ipc';

interface Props {
  response?: ResponseResult;
  error?: AppError;
  sending: boolean;
  nodeId?: string; // 已保存请求的节点 id（"保存为示例"需要）
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

export default function ResponseViewer({ response, error, sending, nodeId }: Props) {
  const [pane, setPane] = useState<'body' | 'preview' | 'headers' | 'tests' | 'timing'>('body');
  const [raw, setRaw] = useState(false);
  const [search, setSearch] = useState('');
  const [exampleSaved, setExampleSaved] = useState(false);
  const [fullBody, setFullBody] = useState<string | null>(null);
  const [loadingFull, setLoadingFull] = useState(false);

  // 新响应到来时丢弃已加载的全文
  useEffect(() => {
    setFullBody(null);
  }, [response]);

  const loadFullBody = async () => {
    if (!response?.body?.blobRef) return;
    setLoadingFull(true);
    try {
      setFullBody(await getResponseBlob(response.body.blobRef));
    } catch (e) {
      setFullBody(`// 加载失败: ${toAppError(e).detail}`);
    } finally {
      setLoadingFull(false);
    }
  };

  const saveAsExample = async () => {
    if (!response || !nodeId) return;
    await upsertExample({
      nodeId,
      name: `${response.status} 示例`,
      status: response.status,
      headers: response.headers,
      body: response.body?.text ?? '',
    } as unknown as Example);
    setExampleSaved(true);
    setTimeout(() => setExampleSaved(false), 1500);
  };

  const pretty = useMemo(() => {
    const source = fullBody ?? response?.body?.text;
    if (!source) return '';
    if (raw) return source;
    try {
      return JSON.stringify(JSON.parse(source), null, 2);
    } catch {
      return source;
    }
  }, [response, raw, fullBody]);

  const contentType = useMemo(
    () =>
      response?.headers?.find((h) => h.key.toLowerCase() === 'content-type')?.value?.toLowerCase() ??
      '',
    [response],
  );
  const previewable = contentType.includes('html') || contentType.startsWith('image/');

  const matchCount = useMemo(() => {
    if (!search) return 0;
    let n = 0;
    let i = -1;
    const lower = pretty.toLowerCase();
    const q = search.toLowerCase();
    while ((i = lower.indexOf(q, i + 1)) !== -1) n++;
    return n;
  }, [pretty, search]);

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

  const tests = response.testResults ?? [];
  const passCount = tests.filter((t) => t.pass).length;
  const testsLabel =
    tests.length > 0
      ? `测试 (${passCount}/${tests.length})`
      : (response.scriptLogs ?? []).length > 0
        ? '测试 (日志)'
        : '测试';

  return (
    <div className="flex flex-col h-full">
      {/* 状态行 */}
      <div className="flex items-center gap-4 px-3 py-2 border-b text-sm">
        <span className={`font-semibold ${statusColor}`}>
          {response.status} {response.statusText}
        </span>
        <span className="text-gray-500">{response.timing.totalMs.toFixed(0)} ms</span>
        <span className="text-gray-500">{formatSize(response.sizeBytes)}</span>
        {tests.length > 0 && (
          <span className={passCount === tests.length ? 'text-green-600' : 'text-red-600'}>
            {passCount === tests.length ? '✓' : '✗'} {passCount}/{tests.length}
          </span>
        )}
        {nodeId && (
          <button
            className="ml-auto text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-0.5"
            onClick={saveAsExample}
            title="保存为示例（供 Mock Server 使用）"
          >
            {exampleSaved ? '已保存 ✓' : '保存为示例'}
          </button>
        )}
      </div>

      {/* 页签 */}
      <div className="flex gap-4 px-3 pt-2 border-b text-sm">
        {(
          [
            ['body', 'Body'],
            ...(previewable ? ([['preview', 'Preview']] as const) : []),
            ['headers', `Headers (${response.headers.length})`],
            ['tests', testsLabel],
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
          <>
            <input
              className="ml-auto mb-1 border rounded px-2 py-0.5 text-xs w-40 outline-none focus:border-blue-400"
              placeholder="搜索 body…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {search && (
              <span className="pb-2 text-xs text-gray-400 self-center">{matchCount} 处</span>
            )}
            <button
              className="pb-2 text-xs text-gray-500 hover:text-gray-800"
              onClick={() => setRaw(!raw)}
            >
              {raw ? 'Pretty' : 'Raw'}
            </button>
          </>
        )}
      </div>

      <div className="flex-1 overflow-auto">
        {pane === 'body' && (
          <div>
            {response.body?.blobRef && !fullBody && (
              <div className="m-3 mb-0 border border-yellow-200 bg-yellow-50 rounded px-3 py-2 text-xs flex items-center gap-2">
                <span className="text-yellow-800">
                  大响应仅显示预览片段（完整 {formatSize(response.sizeBytes)}）
                </span>
                <button
                  className="text-blue-600 hover:underline disabled:opacity-50"
                  disabled={loadingFull}
                  onClick={loadFullBody}
                >
                  {loadingFull ? '加载中…' : '加载完整响应'}
                </button>
              </div>
            )}
            <HighlightedBody text={pretty} query={search} />
          </div>
        )}
        {pane === 'preview' && (
          <PreviewPane text={fullBody ?? response.body?.text ?? ''} contentType={contentType} />
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
        {pane === 'tests' && (
          <TestsPane tests={tests} logs={response.scriptLogs ?? []} />
        )}
        {pane === 'timing' && <TimingBars t={response.timing} />}
      </div>
    </div>
  );
}

// HighlightedBody 带搜索高亮的 body 文本（无匹配时直接渲染，避免大文本分片开销）
function HighlightedBody({ text, query }: { text: string; query: string }) {
  const parts = useMemo(() => {
    if (!query) return null;
    const q = query.toLowerCase();
    const lower = text.toLowerCase();
    const out: { s: string; hit: boolean }[] = [];
    let i = 0;
    let hit = lower.indexOf(q);
    // 上限保护：超过 2000 处只高亮前 2000
    let count = 0;
    while (hit !== -1 && count < 2000) {
      if (hit > i) out.push({ s: text.slice(i, hit), hit: false });
      out.push({ s: text.slice(hit, hit + query.length), hit: true });
      i = hit + query.length;
      hit = lower.indexOf(q, i);
      count++;
    }
    out.push({ s: text.slice(i), hit: false });
    return out;
  }, [text, query]);

  return (
    <pre className="p-3 text-xs font-mono whitespace-pre-wrap break-all">
      {parts
        ? parts.map((p, i) =>
            p.hit ? (
              <mark key={i} className="bg-yellow-200 rounded-sm">
                {p.s}
              </mark>
            ) : (
              p.s
            ),
          )
        : text}
    </pre>
  );
}

// PreviewPane HTML iframe / 图片预览
function PreviewPane({ text, contentType }: { text: string; contentType: string }) {
  if (contentType.startsWith('image/')) {
    // 文本形式的响应体对二进制图片不可靠；SVG 可直接内联
    if (contentType.includes('svg')) {
      return (
        <div className="p-4 flex justify-center">
          <img
            src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(text)}`}
            alt="response preview"
            className="max-w-full max-h-96 border rounded"
          />
        </div>
      );
    }
    return <Center>二进制图片暂不支持预览（可保存为文件查看）</Center>;
  }
  // HTML：沙箱 iframe，禁脚本
  return (
    <iframe
      title="response preview"
      sandbox=""
      srcDoc={text}
      className="w-full h-full border-0 bg-white"
    />
  );
}

function TestsPane({
  tests,
  logs,
}: {
  tests: NonNullable<ResponseResult['testResults']>;
  logs: string[];
}) {
  if (tests.length === 0 && logs.length === 0) {
    return <Center>此请求没有测试脚本；在编辑器"脚本"页签中添加</Center>;
  }
  return (
    <div className="p-3 space-y-3 text-sm">
      {tests.length > 0 && (
        <div className="space-y-1">
          {tests.map((t, i) => (
            <div
              key={i}
              className={`flex items-start gap-2 px-2 py-1.5 rounded ${
                t.pass ? 'bg-green-50' : 'bg-red-50'
              }`}
            >
              <span className={t.pass ? 'text-green-600' : 'text-red-600'}>
                {t.pass ? '✓' : '✗'}
              </span>
              <div className="min-w-0">
                <div className={t.pass ? 'text-green-800' : 'text-red-800'}>{t.name}</div>
                {t.error && (
                  <div className="text-xs text-red-600 font-mono mt-0.5 break-all">{t.error}</div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
      {logs.length > 0 && (
        <div>
          <div className="text-xs text-gray-500 mb-1">控制台输出</div>
          <pre className="bg-gray-50 border rounded p-2 text-xs font-mono whitespace-pre-wrap break-all">
            {logs.join('\n')}
          </pre>
        </div>
      )}
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
