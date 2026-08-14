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
const JSON_SYNTAX_CHAR_LIMIT = 100_000;
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
  // 当前命中的序号（整数下标，-1=无）；搜索词变化时重置
  const [searchIndex, setSearchIndex] = useState(-1);
  const [exampleSaved, setExampleSaved] = useState(false);
  const [savingExample, setSavingExample] = useState(false);
  const [exampleError, setExampleError] = useState('');
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
  const exampleSaveRef = useRef(0);
  activeBlobRef.current = responseBlobRef;

  useEffect(() => {
    exampleSaveRef.current += 1;
    setExampleSaved(false);
    setSavingExample(false);
    setExampleError('');
  }, [nodeId, response]);

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
    if (!response || !nodeId || savingExample) return;
    const saveId = ++exampleSaveRef.current;
    setSavingExample(true);
    setExampleSaved(false);
    setExampleError('');
    try {
      await upsertExample({
        nodeId,
        name: formatMessage('{status} 示例', { status: response.status }),
        status: response.status,
        headers: response.headers,
        body: response.body?.text ?? '',
      } as unknown as Example);
      if (exampleSaveRef.current !== saveId) return;
      setExampleSaved(true);
      window.setTimeout(() => {
        if (exampleSaveRef.current === saveId) setExampleSaved(false);
      }, 1500);
    } catch (cause) {
      if (exampleSaveRef.current === saveId) {
        setExampleError(
          formatMessage('保存示例失败：{detail}', { detail: toAppError(cause).detail }),
        );
      }
    } finally {
      if (exampleSaveRef.current === saveId) setSavingExample(false);
    }
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

  const { pretty, isJsonBody } = useMemo(() => {
    const source = sourceText;
    if (!source) return { pretty: '', isJsonBody: false };
    // 大 JSON 的 parse + stringify 也会阻塞主线程，并可能把文本再膨胀数倍。
    if (raw || source.length > BODY_RENDER_CHAR_LIMIT || binaryBody) {
      return { pretty: source, isJsonBody: false };
    }
    try {
      return { pretty: JSON.stringify(JSON.parse(source), null, 2), isJsonBody: true };
    } catch {
      return { pretty: source, isJsonBody: false };
    }
  }, [sourceText, raw, binaryBody]);

  const renderedBody = useMemo(() => sliceBodyForRender(pretty), [pretty]);

  const previewable = contentType.includes('html') || contentType.startsWith('image/');

  useEffect(() => {
    if (!previewable) setPane((current) => (current === 'preview' ? 'body' : current));
  }, [previewable]);

  const matchCount = useMemo(() => {
    if (!search) return 0;
    let n = 0;
    let i = 0;
    const lower = renderedBody.visibleText.toLowerCase();
    const q = search.toLowerCase();
    let hit = lower.indexOf(q);
    while (hit !== -1 && n < 2000) {
      n++;
      i = hit + q.length;
      hit = lower.indexOf(q, i);
    }
    return n;
  }, [renderedBody.visibleText, search]);

  // 搜索词/响应体变化时重置当前命中；Enter 向前跳、Shift+Enter 向后
  const searchNav = (dir: 1 | -1) => {
    if (!search || matchCount === 0) return;
    setSearchIndex((cur) => {
      if (cur === -1) return dir === 1 ? 0 : matchCount - 1;
      const next = cur + dir;
      if (next < 0) return matchCount - 1;
      if (next >= matchCount) return 0;
      return next;
    });
  };

  // 搜索词或响应体变化时重置当前命中位置
  useEffect(() => {
    setSearchIndex(-1);
  }, [search, renderedBody.visibleText]);

  if (sending) {
    return <SendingProgress progress={progress} />;
  }
  if (error) {
    return (
      <div className="p-4">
        <div className="border border-red-200 bg-red-50 rounded p-3 text-sm">
          <div className="font-medium text-red-700 mb-1">
            {formatMessage(errorKindLabel[error.kind] ?? '错误')}
          </div>
          <div className="text-red-600 font-mono break-all">
            <Verbatim value={error.detail} />
          </div>
        </div>
      </div>
    );
  }
  if (!response) {
    return <Center>{formatMessage('发送请求后在此查看响应')}</Center>;
  }

  const statusColor =
    response.status < 300 ? 'text-green-600' : response.status < 400 ? 'text-yellow-600' : 'text-red-600';

  const tests = response.testResults ?? [];
  const passCount = tests.filter((t) => t.pass).length;
  const testsLabel =
    tests.length > 0
      ? formatMessage('测试 ({passed}/{total})', { passed: passCount, total: tests.length })
      : (response.scriptLogs ?? []).length > 0
        ? formatMessage('测试 (日志)')
        : formatMessage('测试');

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
            className="ml-auto text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-0.5 disabled:cursor-not-allowed disabled:opacity-50"
            onClick={() => void saveAsExample()}
            disabled={savingExample}
            title={formatMessage('保存为示例（供 Mock Server 使用）')}
          >
            {savingExample
              ? formatMessage('保存中…')
              : exampleSaved
                ? formatMessage('已保存 ✓')
                : formatMessage('保存为示例')}
          </button>
        )}
      </div>
      {exampleError && (
        <div className="border-b border-red-100 bg-red-50 px-3 py-1.5 text-xs text-red-600" role="alert">
          <Verbatim value={exampleError} />
        </div>
      )}

      {/* 页签 */}
      <div className="flex gap-4 px-3 pt-2 border-b text-sm">
        {(
          [
            ['body', 'Body'],
            ...(previewable ? ([['preview', 'Preview']] as const) : []),
            ['headers', `Headers (${response.headers.length})`],
            ['tests', testsLabel],
            ['timing', formatMessage('计时')],
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
              placeholder={formatMessage('搜索 body…')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') searchNav(e.shiftKey ? -1 : 1);
              }}
              title={formatMessage('Enter 跳转下一处，Shift+Enter 上一处')}
            />
            {search && (
              <span className="pb-2 text-xs text-gray-400 self-center">
                {matchCount > 0 && searchIndex >= 0 ? `${searchIndex + 1}/${matchCount} ` : ''}
                {matchCount}{formatMessage(' 处')}{renderedBody.omittedChars > 0 ? formatMessage('（当前片段）') : ''}
              </span>
            )}
            <button
              className="pb-2 text-xs text-gray-500 hover:text-gray-800"
              onClick={() => setRaw(!raw)}
            >
              {raw ? 'Pretty' : 'Raw'}
            </button>
            <button
              className="pb-2 text-xs text-gray-500 hover:text-gray-800"
              title={formatMessage('复制响应体')}
              onClick={() => void navigator.clipboard.writeText(renderedBody.visibleText).catch(() => {})}
            >
              {formatMessage('复制')}
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
                  {formatMessage('已加载 ')}{formatSize(blobBytes.byteLength)} / {formatSize(blobSize || response.sizeBytes)}
                </span>
                {!blobEof && blobBytes.byteLength < BODY_INSPECT_BYTE_LIMIT && (
                  <button
                    className="text-blue-600 hover:underline disabled:opacity-50"
                    disabled={loadingChunk}
                    onClick={() => void loadNextChunk()}
                  >
                    {loadingChunk ? formatMessage('加载中…') : formatMessage('加载下一块')}
                  </button>
                )}
                {!blobEof && blobBytes.byteLength >= BODY_INSPECT_BYTE_LIMIT && (
                  <span className="text-gray-500">{formatMessage('已达到片段读取上限')}</span>
                )}
                <button
                  className="text-blue-600 hover:underline disabled:opacity-50"
                  disabled={saving}
                  onClick={() => void saveBlobAs()}
                >
                  {saving ? formatMessage('保存中…') : formatMessage('另存为…')}
                </button>
                {saveMessage && <span className="text-gray-600"><Verbatim value={saveMessage} /></span>}
                {blobError && <span className="basis-full text-red-600"><Verbatim value={blobError} /></span>}
              </div>
            )}
            <HighlightedBody
              body={renderedBody}
              query={search}
              isJson={isJsonBody && renderedBody.visibleText.length <= JSON_SYNTAX_CHAR_LIMIT}
              currentHit={searchIndex}
              onHitScroll={(el) => el?.scrollIntoView({ block: 'center', behavior: 'smooth' })}
            />
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
          <div className="m-3">
            <button
              className="mb-2 text-xs text-gray-500 hover:text-gray-800"
              onClick={() =>
                void navigator.clipboard
                  .writeText(response.headers.map((h) => `${h.key}: ${h.value}`).join('\n'))
                  .catch(() => {})
              }
            >
              {formatMessage('复制全部')}
            </button>
            <table className="w-full text-sm">
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
          </div>
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

// JSON token 着色类映射（Tailwind 内联色，避免主题样式冲突）
const JSON_CLASS: Record<string, string> = {
  key: 'text-purple-600',
  string: 'text-green-700',
  number: 'text-orange-600',
  bool: 'text-blue-600',
  null: 'text-blue-600',
  punct: 'text-gray-400',
};

// 轻量 JSON 分词：字符串/数字/布尔/null/标点；不依赖外部库。
// 返回带 cls 的分片（key 通过"字符串后紧跟冒号"判定）。
function tokenizeJson(text: string): { s: string; cls: string }[] {
  const out: { s: string; cls: string }[] = [];
  const re = /"(?:\\.|[^"\\])*"|true(?=\s*(?:[,\]}]|$))|false(?=\s*(?:[,\]}]|$))|null(?=\s*(?:[,\]}]|$))|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|[{}[\],:]|\s+|[^"{}[\],:\s]+/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    const tok = m[0];
    if (tok === '"' || tok === '{' || tok === '}' || tok === '[' || tok === ']' || tok === ',' || tok === ':') {
      out.push({ s: tok, cls: 'punct' });
    } else if (tok.startsWith('"')) {
      // 向后看：跳过空白后是否为 ':' → key，否则为字符串值
      const rest = text.slice(re.lastIndex);
      const after = rest.match(/^\s*:/);
      out.push({ s: tok, cls: after ? 'key' : 'string' });
    } else if (/^-?\d/.test(tok)) {
      out.push({ s: tok, cls: 'number' });
    } else if (tok === 'true' || tok === 'false') {
      out.push({ s: tok, cls: 'bool' });
    } else if (tok === 'null') {
      out.push({ s: tok, cls: 'null' });
    } else {
      // 空白或异常字符：原样保留，不着色
      out.push({ s: tok, cls: 'plain' });
    }
  }
  return out;
}

// HighlightedBody 只接收有界片段，搜索高亮不会意外把截断区重新带回 DOM。
function HighlightedBody({
  body,
  query,
  isJson,
  currentHit,
  onHitScroll,
}: {
  body: BodyRenderSlice;
  query: string;
  isJson: boolean;
  /** 当前命中的序号（0 起）；用于视觉强调并滚动到该处（由 onHitScroll 实现） */
  currentHit?: number;
  onHitScroll?: (el: HTMLElement | null) => void;
}) {
  const { visibleText, omittedChars } = body;
  // 预先计算搜索命中区间，JSON 着色时用区间叠加高亮，避免正则双重切分不一致。
  const hitRanges = useMemo(() => {
    if (!query) return [] as [number, number][];
    const q = query.toLowerCase();
    const lower = visibleText.toLowerCase();
    const out: [number, number][] = [];
    let i = 0;
    let hit = lower.indexOf(q);
    let count = 0;
    while (hit !== -1 && count < 2000) {
      out.push([hit, hit + query.length]);
      i = hit + query.length;
      hit = lower.indexOf(q, i);
      count++;
    }
    return out;
  }, [visibleText, query]);

  // 当前命中的 <mark> 引用：命中序号有效且文本变化后滚动定位。
  const currentMarkRef = useRef<HTMLElement | null>(null);
  const focusTarget = useMemo(() => {
    if (!query || currentHit == null || currentHit < 0 || currentHit >= hitRanges.length) return null;
    return hitRanges[currentHit];
  }, [query, currentHit, hitRanges]);
  useEffect(() => {
    if (focusTarget && onHitScroll) onHitScroll(currentMarkRef.current);
  }, [focusTarget, onHitScroll]);

  const segments = useMemo(() => {
    // 统一先分词：JSON 走语法着色，否则整体当作一个 plain 片段
    const toks = isJson ? tokenizeJson(visibleText) : [{ s: visibleText, cls: 'plain' }];
    if (hitRanges.length === 0) return toks.map((t) => ({ ...t, hit: false, hitIdx: -1 }));
    // 按命中区间切分每个 token，命中部分标记 hit.idx（当前命中高亮用）
    const out: { s: string; cls: string; hit: boolean; hitIdx: number }[] = [];
    let offset = 0;
    let firstHit = 0;
    for (const t of toks) {
      const start = offset;
      const end = offset + t.s.length;
      offset = end;
      // 收集与 token 相交的命中区间
      while (firstHit < hitRanges.length && hitRanges[firstHit][1] <= start) firstHit++;
      const local: { hs: number; he: number; idx: number }[] = [];
      for (let k = firstHit; k < hitRanges.length; k++) {
        const [hs, he] = hitRanges[k];
        if (hs >= end) break;
        if (he > start && hs < end) local.push({ hs, he, idx: k });
      }
      if (local.length === 0) {
        out.push({ s: t.s, cls: t.cls, hit: false, hitIdx: -1 });
        continue;
      }
      let pos = start;
      for (const { hs, he, idx } of local) {
        const cs = Math.max(hs, start);
        const ce = Math.min(he, end);
        if (cs > pos) out.push({ s: visibleText.slice(pos, cs), cls: t.cls, hit: false, hitIdx: -1 });
        out.push({ s: visibleText.slice(cs, ce), cls: t.cls, hit: true, hitIdx: idx });
        pos = ce;
      }
      if (pos < end) out.push({ s: visibleText.slice(pos, end), cls: t.cls, hit: false, hitIdx: -1 });
    }
    return out;
  }, [visibleText, hitRanges, isJson]);

  return (
    <>
      {omittedChars > 0 && (
        <div className="m-3 mb-0 border border-orange-200 bg-orange-50 rounded px-3 py-2 text-xs text-orange-800">
          {formatMessage('响应体过大，仅渲染前 ')}{visibleText.length.toLocaleString()}{formatMessage(' 个字符，另有')}{' '}
          {omittedChars.toLocaleString()}{formatMessage(' 个字符未显示。')}
        </div>
      )}
      <pre className="p-3 text-xs font-mono whitespace-pre-wrap break-all">
        {segments.map((p, i) =>
          p.hit ? (
            <mark
              key={i}
              ref={currentHit != null && p.hitIdx === currentHit ? (el) => { currentMarkRef.current = el; } : undefined}
              className={
                currentHit != null && p.hitIdx === currentHit
                  ? 'bg-orange-300 rounded-sm'
                  : 'bg-yellow-200 rounded-sm'
              }
            >
              <span className={isJson ? JSON_CLASS[p.cls] : undefined}>
                <Verbatim value={p.s} />
              </span>
            </mark>
          ) : isJson ? (
            <span key={i} className={isJson ? JSON_CLASS[p.cls] : undefined}>
              <Verbatim value={p.s} />
            </span>
          ) : (
            <Verbatim key={i} value={p.s} />
          ),
        )}
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
        return <Center>{formatMessage('响应体过大，Preview 暂不渲染（请使用 Body 片段查看）')}</Center>;
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
            {loading ? formatMessage('加载图片中…') : formatMessage('加载图片预览')}
          </button>
          {error && <span className="text-xs text-red-600"><Verbatim value={error} /></span>}
        </div>
      );
    }
    return <Center>{formatMessage('图片数据不可用')}</Center>;
  }
  if (hasBlob) {
    return <Center>{formatMessage('大型 HTML 响应不加载到 Preview，请使用 Body 片段或另存为查看')}</Center>;
  }
  if (text.length > BODY_RENDER_CHAR_LIMIT) {
    return <Center>{formatMessage('响应体过大，Preview 暂不渲染（请使用 Body 片段查看）')}</Center>;
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
    return <Center>{formatMessage('此请求没有测试脚本；在编辑器"脚本"页签中添加')}</Center>;
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
          <div className="text-xs text-gray-500 mb-1">{formatMessage('控制台输出')}</div>
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
    [formatMessage('连接'), t.connectMs],
    ['TLS', t.tlsMs],
    [formatMessage('首字节 (TTFB)'), t.ttfbMs],
    [formatMessage('下载'), t.downloadMs],
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
        <span className="w-28 font-medium">{formatMessage('总计')}</span>
        <span className="text-gray-700">{t.totalMs.toFixed(1)} ms</span>
      </div>
    </div>
  );
}

function SendingProgress({ progress }: { progress?: RequestProgress }) {
  const phase = progress?.phase ?? 'sending';
  const labels: Record<RequestProgress['phase'], string> = {
    sending: formatMessage('正在建立连接…'),
    ttfb: formatMessage('已收到响应头，等待响应体…'),
    downloading: formatMessage('正在下载响应…'),
    done: formatMessage('请求完成'),
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
