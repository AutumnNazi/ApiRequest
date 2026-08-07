// 响应查看器：状态行 + Body(Pretty/Raw)/Headers 页签 + 分阶段计时
import { memo, useEffect, useMemo, useRef, useState } from 'react';
import {
  upsertExample,
  getResponseBlobInfo,
  readResponseBlobRange,
  saveResponseBlob,
  saveNativeFile,
  toAppError,
  type ResponseResult,
  type AppError,
  type Example,
  type RequestProgress,
} from '../ipc';
import { formatMessage, useLocale, Verbatim } from '../i18n/locale';

interface Props {
  response?: ResponseResult;
  error?: AppError;
  sending: boolean;
  progress?: RequestProgress;
  nodeId?: string; // 已保存请求的节点 id（"保存为示例"需要）
}

const BODY_RENDER_CHAR_LIMIT = 500_000;
const BLOB_CHUNK_SIZE = 256 << 10;
const BODY_INSPECT_BYTE_LIMIT = BODY_RENDER_CHAR_LIMIT * 4;
const IMAGE_PREVIEW_BYTE_LIMIT = 16 << 20;

const errorKindLabel: Record<string, string> = {
  network: '网络错误',
  tls: 'TLS 错误',
  validation: '请求无效',
  storage: '存储错误',
  script: '脚本错误',
  import: '导入错误',
  unknown: '错误',
};

// memo：App 在编辑请求草稿时频繁重渲染，ResponseViewer 仅依赖响应数据，无需跟着重渲染
const ResponseViewer = memo(function ResponseViewer({ response, error, sending, progress, nodeId }: Props) {
  const locale = useLocale((state) => state.locale);
  const [pane, setPane] = useState<'body' | 'preview' | 'headers' | 'tests' | 'timing'>('body');
  const [raw, setRaw] = useState(false);
  const [search, setSearch] = useState('');
  const [exampleSaved, setExampleSaved] = useState(false);
  const [blobBytes, setBlobBytes] = useState<Uint8Array>(() => new Uint8Array());
  const [blobSize, setBlobSize] = useState(0);
  const [blobEof, setBlobEof] = useState(false);
  const [loadingChunk, setLoadingChunk] = useState(false);
  const [blobError, setBlobError] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveMessage, setSaveMessage] = useState('');
  const [imageUrl, setImageUrl] = useState('');
  const responseBlobRef = response?.body?.blobRef ?? '';
  const activeBlobRef = useRef(responseBlobRef);
  const loadingBlobRef = useRef('');
  activeBlobRef.current = responseBlobRef;

  // 新响应到来时丢弃旧 chunk，并只读取 metadata。
  useEffect(() => {
    let cancelled = false;
    setBlobBytes(new Uint8Array());
    setBlobSize(response?.sizeBytes ?? 0);
    setBlobEof(false);
    setBlobError('');
    setSaveMessage('');
    setLoadingChunk(false);
    setSaving(false);
    loadingBlobRef.current = '';
    const ref = responseBlobRef;
    if (ref) {
      void getResponseBlobInfo(ref)
        .then((info) => {
          if (!cancelled) setBlobSize(info.sizeBytes);
        })
        .catch((error) => {
          if (!cancelled) setBlobError(toAppError(error).detail);
        });
    }
    return () => {
      cancelled = true;
    };
  }, [response, responseBlobRef]);

  const loadNextChunk = async () => {
    const ref = response?.body?.blobRef;
    if (!ref || loadingBlobRef.current || blobEof || blobBytes.byteLength >= BODY_INSPECT_BYTE_LIMIT) {
      return;
    }
    loadingBlobRef.current = ref;
    setLoadingChunk(true);
    setBlobError('');
    try {
      const chunk = await readResponseBlobRange(
        ref,
        blobBytes.byteLength,
        Math.min(BLOB_CHUNK_SIZE, BODY_INSPECT_BYTE_LIMIT - blobBytes.byteLength),
      );
      if (activeBlobRef.current !== ref) return;
      const next = concatBytes(blobBytes, decodeBase64(chunk.dataBase64));
      setBlobBytes(next);
      setBlobEof(chunk.eof);
    } catch (error) {
      if (activeBlobRef.current === ref) setBlobError(toAppError(error).detail);
    } finally {
      if (loadingBlobRef.current === ref) {
        loadingBlobRef.current = '';
        setLoadingChunk(false);
      }
    }
  };

  const loadImagePreview = async () => {
    const ref = response?.body?.blobRef;
    if (!ref || loadingBlobRef.current || blobEof) return;
    if (blobSize > IMAGE_PREVIEW_BYTE_LIMIT) {
      setBlobError(
        formatMessage('图片为 {size}，超过 {limit} 预览上限，请另存为查看。', {
          size: formatSize(blobSize),
          limit: formatSize(IMAGE_PREVIEW_BYTE_LIMIT),
        }),
      );
      return;
    }
    loadingBlobRef.current = ref;
    setLoadingChunk(true);
    setBlobError('');
    try {
      let next = blobBytes;
      let eof: boolean = blobEof;
      while (!eof && next.byteLength < blobSize) {
        const chunk = await readResponseBlobRange(
          ref,
          next.byteLength,
          Math.min(1 << 20, blobSize - next.byteLength),
        );
        if (activeBlobRef.current !== ref) return;
        next = concatBytes(next, decodeBase64(chunk.dataBase64));
        eof = chunk.eof;
      }
      setBlobBytes(next);
      setBlobEof(eof);
    } catch (error) {
      if (activeBlobRef.current === ref) setBlobError(toAppError(error).detail);
    } finally {
      if (loadingBlobRef.current === ref) {
        loadingBlobRef.current = '';
        setLoadingChunk(false);
      }
    }
  };

  const saveBlobAs = async () => {
    const ref = response?.body?.blobRef;
    if (!ref || saving) return;
    setSaving(true);
    setSaveMessage('');
    try {
      const destination = await saveNativeFile('保存响应', suggestedResponseFilename(response));
      if (!destination) return;
      const written = await saveResponseBlob(ref, destination);
      if (activeBlobRef.current === ref) {
        setSaveMessage(formatMessage('已保存 {size}', { size: formatSize(written) }));
      }
    } catch (error) {
      if (activeBlobRef.current === ref) {
        setSaveMessage(formatMessage('保存失败：{detail}', { detail: toAppError(error).detail }));
      }
    } finally {
      if (activeBlobRef.current === ref) setSaving(false);
    }
  };

  const saveAsExample = async () => {
    if (!response || !nodeId) return;
    await upsertExample({
      nodeId,
      name: formatMessage('{status} 示例', { status: response.status }),
      status: response.status,
      headers: response.headers,
      body: response.body?.text ?? '',
    } as unknown as Example);
    setExampleSaved(true);
    setTimeout(() => setExampleSaved(false), 1500);
  };

  const contentType = useMemo(
    () =>
      response?.headers?.find((h) => h.key.toLowerCase() === 'content-type')?.value?.toLowerCase() ??
      '',
    [response],
  );
  const binaryBody = response?.body?.encoding === 'base64' || response?.body?.encoding === 'binary';
  const inlineBinaryBytes = useMemo(
    () =>
      response?.body?.encoding === 'base64' && response.body.text
        ? decodeBase64(response.body.text)
        : new Uint8Array(),
    [response],
  );
  const sourceText = useMemo(() => {
    if (binaryBody) {
      const bytes = blobBytes.byteLength > 0 ? blobBytes : inlineBinaryBytes;
      return bytes.byteLength > 0 ? hexPreview(bytes) : '[binary response]';
    }
    if (blobBytes.byteLength > 0) return new TextDecoder().decode(blobBytes);
    return response?.body?.text ?? '';
  }, [binaryBody, blobBytes, inlineBinaryBytes, locale, response]);

  const completeImageBytes = contentType.startsWith('image/')
    ? response?.body?.encoding === 'base64'
      ? inlineBinaryBytes
      : blobEof
        ? blobBytes
        : null
    : null;

  useEffect(() => {
    setImageUrl('');
    if (!completeImageBytes || completeImageBytes.byteLength === 0) return;
    const data = completeImageBytes.slice().buffer;
    const url = URL.createObjectURL(new Blob([data], { type: contentType || 'application/octet-stream' }));
    setImageUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [completeImageBytes, contentType]);

  const pretty = useMemo(() => {
    const source = sourceText;
    if (!source) return '';
    // 大 JSON 的 parse + stringify 也会阻塞主线程，并可能把文本再膨胀数倍。
    if (raw || source.length > BODY_RENDER_CHAR_LIMIT) return source;
    try {
      return JSON.stringify(JSON.parse(source), null, 2);
    } catch {
      return source;
    }
  }, [sourceText, raw]);

  const renderedBody = useMemo(() => sliceBodyForRender(pretty), [pretty]);

  const previewable = contentType.includes('html') || contentType.startsWith('image/');

  const matchCount = useMemo(() => {
    if (!search) return 0;
    let n = 0;
    let i = -1;
    const lower = renderedBody.visibleText.toLowerCase();
    const q = search.toLowerCase();
    while ((i = lower.indexOf(q, i + 1)) !== -1) n++;
    return n;
  }, [renderedBody.visibleText, search]);

  if (sending) {
    return <SendingProgress progress={progress} />;
  }
  if (error) {
    return (
      <div className="p-4">
        <div className="border border-red-200 bg-red-50 rounded p-3 text-sm">
          <div className="font-medium text-red-700 mb-1">
            {errorKindLabel[error.kind] ?? '错误'}
          </div>
          <div className="text-red-600 font-mono break-all">
            <Verbatim value={error.detail} />
          </div>
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
      ? formatMessage('测试 ({passed}/{total})', { passed: passCount, total: tests.length })
      : (response.scriptLogs ?? []).length > 0
        ? '测试 (日志)'
        : '测试';

  return (
    <div className="flex flex-col h-full">
      {/* 状态行 */}
      <div className="flex items-center gap-4 px-3 py-2 border-b text-sm">
        <span className={`font-semibold ${statusColor}`}>
          {response.status} <Verbatim value={response.statusText} />
        </span>
        <span className="text-gray-500">{response.timing.totalMs.toFixed(0)} ms</span>
        <span className="text-gray-500">{formatSize(response.sizeBytes)}</span>
        {tests.length > 0 && (
          <span className={passCount === tests.length ? 'text-green-600' : 'text-red-600'}>
            {passCount === tests.length ? '✓' : '✗'} {passCount}/{tests.length}
          </span>
        )}
        {nodeId && !response.body?.blobRef && (
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
              <span className="pb-2 text-xs text-gray-400 self-center">
                {matchCount} 处{renderedBody.omittedChars > 0 ? '（当前片段）' : ''}
              </span>
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
            {response.body?.blobRef && (
              <div className="m-3 mb-0 border border-yellow-200 bg-yellow-50 rounded px-3 py-2 text-xs flex flex-wrap items-center gap-2">
                <span className="text-yellow-800">
                  已加载 {formatSize(blobBytes.byteLength)} / {formatSize(blobSize || response.sizeBytes)}
                </span>
                {!blobEof && blobBytes.byteLength < BODY_INSPECT_BYTE_LIMIT && (
                  <button
                    className="text-blue-600 hover:underline disabled:opacity-50"
                    disabled={loadingChunk}
                    onClick={() => void loadNextChunk()}
                  >
                    {loadingChunk ? '加载中…' : '加载下一块'}
                  </button>
                )}
                {!blobEof && blobBytes.byteLength >= BODY_INSPECT_BYTE_LIMIT && (
                  <span className="text-gray-500">已达到片段读取上限</span>
                )}
                <button
                  className="text-blue-600 hover:underline disabled:opacity-50"
                  disabled={saving}
                  onClick={() => void saveBlobAs()}
                >
                  {saving ? '保存中…' : '另存为…'}
                </button>
                {saveMessage && <span className="text-gray-600"><Verbatim value={saveMessage} /></span>}
                {blobError && <span className="basis-full text-red-600"><Verbatim value={blobError} /></span>}
              </div>
            )}
            <HighlightedBody body={renderedBody} query={search} />
          </div>
        )}
        {pane === 'preview' && (
          <PreviewPane
            text={binaryBody ? '' : sourceText}
            contentType={contentType}
            imageUrl={imageUrl}
            hasBlob={Boolean(response.body?.blobRef)}
            loading={loadingChunk}
            error={blobError}
            onLoadImage={() => void loadImagePreview()}
          />
        )}
        {pane === 'headers' && (
          <table className="w-full text-sm m-3">
            <tbody>
              {response.headers.map((h, i) => (
                <tr key={i} className="border-b border-gray-100">
                  <td className="p-1 pr-4 font-medium text-gray-700 whitespace-nowrap align-top">
                    <Verbatim value={h.key} />
                  </td>
                  <td className="p-1 font-mono text-xs break-all"><Verbatim value={h.value} /></td>
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
});

export default ResponseViewer;

interface BodyRenderSlice {
  visibleText: string;
  omittedChars: number;
}

function sliceBodyForRender(text: string): BodyRenderSlice {
  if (text.length <= BODY_RENDER_CHAR_LIMIT) return { visibleText: text, omittedChars: 0 };

  let end = BODY_RENDER_CHAR_LIMIT;
  const lastCodeUnit = text.charCodeAt(end - 1);
  if (lastCodeUnit >= 0xd800 && lastCodeUnit <= 0xdbff) end--;
  return { visibleText: text.slice(0, end), omittedChars: text.length - end };
}

// HighlightedBody 只接收有界片段，搜索高亮不会意外把截断区重新带回 DOM。
function HighlightedBody({ body, query }: { body: BodyRenderSlice; query: string }) {
  const { visibleText, omittedChars } = body;
  const parts = useMemo(() => {
    if (!query) return null;
    const q = query.toLowerCase();
    const lower = visibleText.toLowerCase();
    const out: { s: string; hit: boolean }[] = [];
    let i = 0;
    let hit = lower.indexOf(q);
    // 上限保护：超过 2000 处只高亮前 2000
    let count = 0;
    while (hit !== -1 && count < 2000) {
      if (hit > i) out.push({ s: visibleText.slice(i, hit), hit: false });
      out.push({ s: visibleText.slice(hit, hit + query.length), hit: true });
      i = hit + query.length;
      hit = lower.indexOf(q, i);
      count++;
    }
    out.push({ s: visibleText.slice(i), hit: false });
    return out;
  }, [visibleText, query]);

  return (
    <>
      {omittedChars > 0 && (
        <div className="m-3 mb-0 border border-orange-200 bg-orange-50 rounded px-3 py-2 text-xs text-orange-800">
          响应体过大，仅渲染前 {visibleText.length.toLocaleString()} 个字符，另有{' '}
          {omittedChars.toLocaleString()} 个字符未显示。
        </div>
      )}
      <pre className="p-3 text-xs font-mono whitespace-pre-wrap break-all">
        {parts
          ? parts.map((p, i) =>
              p.hit ? (
                <mark key={i} className="bg-yellow-200 rounded-sm">
                  <Verbatim value={p.s} />
                </mark>
              ) : (
                <Verbatim key={i} value={p.s} />
              ),
            )
          : <Verbatim value={visibleText} />}
      </pre>
    </>
  );
}

// PreviewPane HTML iframe / binary-safe 图片预览。
function PreviewPane({
  text,
  contentType,
  imageUrl,
  hasBlob,
  loading,
  error,
  onLoadImage,
}: {
  text: string;
  contentType: string;
  imageUrl: string;
  hasBlob: boolean;
  loading: boolean;
  error: string;
  onLoadImage(): void;
}) {
  if (contentType.startsWith('image/')) {
    if (!hasBlob && contentType.includes('svg') && text) {
      if (text.length > BODY_RENDER_CHAR_LIMIT) {
        return <Center>响应体过大，Preview 暂不渲染（请使用 Body 片段查看）</Center>;
      }
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
    if (imageUrl) {
      return (
        <div className="p-4 flex justify-center">
          <img
            src={imageUrl}
            alt="response preview"
            className="max-w-full max-h-96 border rounded"
          />
        </div>
      );
    }
    if (hasBlob) {
      return (
        <div className="h-full flex flex-col items-center justify-center gap-2 text-sm">
          <button
            className="border rounded px-3 py-1.5 text-blue-600 hover:bg-blue-50 disabled:opacity-50"
            disabled={loading}
            onClick={onLoadImage}
          >
            {loading ? '加载图片中…' : '加载图片预览'}
          </button>
          {error && <span className="text-xs text-red-600"><Verbatim value={error} /></span>}
        </div>
      );
    }
    return <Center>图片数据不可用</Center>;
  }
  if (hasBlob) {
    return <Center>大型 HTML 响应不加载到 Preview，请使用 Body 片段或另存为查看</Center>;
  }
  if (text.length > BODY_RENDER_CHAR_LIMIT) {
    return <Center>响应体过大，Preview 暂不渲染（请使用 Body 片段查看）</Center>;
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
                <div className={t.pass ? 'text-green-800' : 'text-red-800'}><Verbatim value={t.name} /></div>
                {t.error && (
                  <div className="text-xs text-red-600 font-mono mt-0.5 break-all"><Verbatim value={t.error} /></div>
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
            <Verbatim value={logs.join('\n')} />
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

function SendingProgress({ progress }: { progress?: RequestProgress }) {
  const phase = progress?.phase ?? 'sending';
  const labels: Record<RequestProgress['phase'], string> = {
    sending: '正在建立连接…',
    ttfb: '已收到响应头，等待响应体…',
    downloading: '正在下载响应…',
    done: '请求完成',
  };
  const received = progress?.bytesReceived ?? 0;
  const total = progress?.totalBytes ?? 0;
  const percent = total > 0 ? Math.min((received / total) * 100, 100) : 0;
  return (
    <div className="h-full flex flex-col items-center justify-center gap-3 text-sm text-gray-500">
      <span>{labels[phase]}</span>
      <div className="w-64 h-1.5 rounded bg-gray-100 overflow-hidden">
        <div
          className={`h-full bg-blue-500 ${total <= 0 ? 'w-1/3 animate-pulse' : ''}`}
          style={total > 0 ? { width: `${percent}%` } : undefined}
        />
      </div>
      {(phase === 'downloading' || received > 0) && (
        <span className="text-xs text-gray-400">
          {formatSize(received)}{total > 0 ? ` / ${formatSize(total)} (${percent.toFixed(0)}%)` : ''}
        </span>
      )}
    </div>
  );
}

function Center({ children }: { children: React.ReactNode }) {
  return (
    <div className="h-full flex items-center justify-center text-gray-400 text-sm">{children}</div>
  );
}

function decodeBase64(value: string): Uint8Array {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function concatBytes(left: Uint8Array, right: Uint8Array): Uint8Array {
  const combined = new Uint8Array(left.byteLength + right.byteLength);
  combined.set(left, 0);
  combined.set(right, left.byteLength);
  return combined;
}

function hexPreview(bytes: Uint8Array): string {
  const visible = bytes.subarray(0, Math.min(bytes.byteLength, 64 << 10));
  const lines: string[] = [];
  for (let offset = 0; offset < visible.byteLength; offset += 16) {
    const row = visible.subarray(offset, offset + 16);
    const hex = Array.from(row, (value) => value.toString(16).padStart(2, '0')).join(' ');
    const ascii = Array.from(row, (value) => (value >= 32 && value <= 126 ? String.fromCharCode(value) : '.')).join('');
    lines.push(`${offset.toString(16).padStart(8, '0')}  ${hex.padEnd(47)}  ${ascii}`);
  }
  if (bytes.byteLength > visible.byteLength) {
    lines.push(formatMessage('\n… {size} 未显示', { size: formatSize(bytes.byteLength - visible.byteLength) }));
  }
  return lines.join('\n');
}

function suggestedResponseFilename(response: ResponseResult): string {
  const contentType =
    response.headers.find((header) => header.key.toLowerCase() === 'content-type')?.value.toLowerCase() ?? '';
  const extensions: [string, string][] = [
    ['application/json', 'json'],
    ['text/html', 'html'],
    ['text/plain', 'txt'],
    ['application/xml', 'xml'],
    ['image/png', 'png'],
    ['image/jpeg', 'jpg'],
    ['image/gif', 'gif'],
    ['image/webp', 'webp'],
    ['image/svg+xml', 'svg'],
    ['application/pdf', 'pdf'],
  ];
  const extension = extensions.find(([mime]) => contentType.includes(mime))?.[1] ?? 'bin';
  return `response-${Date.now()}.${extension}`;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}
