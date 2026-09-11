// 响应对比：当前响应与历史记录做统一行 diff（docs/frontend.md）。
// JSON 侧默认"格式归一"（排序键 + pretty），消除缩进/键序噪声；超限回退并排原文。
import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import ModalFrame from './ModalFrame';
import {
  listHistory,
  getHistory,
  readResponseBlobRange,
  listExamples,
  toAppError,
  type HistorySummary,
  type Example,
} from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';
import { diffLines, normalizeJsonText, type DiffRow } from '../utils/lineDiff';

// 对比任一侧的正文上限：超出后不做行 diff（LWS DP 代价与 UI 渲染都不可控）
const MAX_DIFF_BODY_CHARS = 400_000;
const BLOB_LOAD_LIMIT = 2 << 20; // blob 侧最多读 2 MiB

export interface DiffSide {
  label: string;
  status: number;
  durationMs: number;
  sizeBytes: number;
  bodyText: string;
  blobRef?: string;
}

function decodeBase64(value: string): Uint8Array {
  const raw = atob(value);
  const bytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
  return bytes;
}

// blob 引用 → 文本（分块读，超上限截断）
async function loadBlobText(blobRef: string): Promise<{ text: string; truncated: boolean }> {
  const decoder = new TextDecoder('utf-8');
  let loaded = 0;
  let eof = false;
  const parts: string[] = [];
  while (!eof && loaded < BLOB_LOAD_LIMIT) {
    const chunk = await readResponseBlobRange(blobRef, loaded, Math.min(1 << 20, BLOB_LOAD_LIMIT - loaded));
    parts.push(decoder.decode(decodeBase64(chunk.dataBase64), { stream: true }));
    loaded += decodeBase64(chunk.dataBase64).byteLength;
    eof = chunk.eof;
  }
  return { text: parts.join(''), truncated: !eof };
}

export default function ResponseDiffDialog({
  workspaceId,
  nodeId,
  base,
  onClose,
}: {
  workspaceId: string;
  nodeId?: string; // 有节点 id 时支持对照"保存的示例"（API 漂移检测）
  base: DiffSide;
  onClose(): void;
}) {
  const [source, setSource] = useState<'history' | 'example'>('history');
  const [search, setSearch] = useState(base.label);
  const [picked, setPicked] = useState<HistorySummary | null>(null);
  const [pickedExample, setPickedExample] = useState<Example | null>(null);
  // 轻量投影：避免把 wails 模型类实例塞进 state（需要 convertValues）
  const [other, setOther] = useState<{ status: number; durationMs: number; bodyInline: string; createdAt: number } | null>(null);
  const [normalize, setNormalize] = useState(true);
  const [loadingSide, setLoadingSide] = useState(false);
  const [sideError, setSideError] = useState('');

  const history = useQuery({
    queryKey: ['history-diff', workspaceId, search.trim()],
    queryFn: () => listHistory(workspaceId, { search: search.trim(), limit: 30 }),
    enabled: source === 'history',
  });
  const examples = useQuery({
    queryKey: ['examples', nodeId],
    queryFn: () => listExamples(nodeId ?? ''),
    enabled: source === 'example' && !!nodeId,
  });

  const pickExample = (example: Example) => {
    setPickedExample(example);
    setSideError('');
    setOther({
      status: example.status,
      durationMs: 0,
      bodyInline: example.body ?? '',
      createdAt: example.updatedAt,
    });
  };

  const pick = async (item: HistorySummary) => {
    setPicked(item);
    setSideError('');
    setLoadingSide(true);
    try {
      const detail = await getHistory(workspaceId, item.id);
      let text = detail.bodyInline ?? '';
      if (!text && detail.bodyRef) {
        text = (await loadBlobText(detail.bodyRef)).text;
      }
      setOther({ status: detail.status, durationMs: detail.durationMs, bodyInline: text, createdAt: detail.createdAt });
    } catch (cause) {
      setSideError(toAppError(cause).detail);
      setPicked(null);
      setPickedExample(null);
    } finally {
      setLoadingSide(false);
    }
  };

  const rows: DiffRow[] | null = useMemo(() => {
    if (!other) return null;
    let a = base.bodyText;
    let b = other.bodyInline ?? '';
    if (normalize) {
      const na = normalizeJsonText(a);
      const nb = normalizeJsonText(b);
      if (na !== null && nb !== null) {
        a = na;
        b = nb;
      }
    }
    if (a.length > MAX_DIFF_BODY_CHARS || b.length > MAX_DIFF_BODY_CHARS) return null;
    return diffLines(a, b);
  }, [other, normalize, base.bodyText]);

  const tooLarge = other !== null && rows === null;

  return (
    <ModalFrame className="w-[720px] max-w-[92vw] h-[70vh]" onClose={onClose} titleId="response-diff-title">
      <div className="flex flex-col h-full" role="dialog" aria-labelledby="response-diff-title">
        <div className="px-4 py-3 border-b">
          <h2 id="response-diff-title" className="text-sm font-semibold">
            {formatMessage('响应对比')}
          </h2>
          <p className="text-xs text-gray-500 mt-1">
            {formatMessage('当前')}：<span data-side="base-status">{base.status}</span> · {base.durationMs}ms · <Verbatim value={base.label} />
            {picked && other && (
              <>
                {' ↔ '}
                {pickedExample ? formatMessage('示例') : formatMessage('历史')}：<span data-side="other-status">{other.status}</span> · {other.durationMs}ms · {other.createdAt > 0 ? new Date(other.createdAt).toLocaleString() : ''}
              </>
            )}
          </p>
        </div>

        {!picked && !pickedExample && (
          <div className="flex-1 flex flex-col min-h-0">
            <div className="px-4 py-2 border-b flex items-center gap-2">
              <button
                className={`border rounded px-2 py-0.5 text-xs ${source === 'history' ? 'bg-blue-50 border-blue-300 text-blue-700' : 'text-gray-500'}`}
                onClick={() => setSource('history')}
              >
                {formatMessage('历史')}
              </button>
              {nodeId && (
                <button
                  className={`border rounded px-2 py-0.5 text-xs ${source === 'example' ? 'bg-blue-50 border-blue-300 text-blue-700' : 'text-gray-500'}`}
                  onClick={() => setSource('example')}
                >
                  {formatMessage('示例')}
                </button>
              )}
              {source === 'history' && (
                <input
                  className="flex-1 border rounded px-2 py-1 text-xs outline-none focus:border-blue-400"
                  placeholder={formatMessage('搜索 URL / 方法…')}
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              )}
            </div>
            <div className="flex-1 overflow-auto text-xs">
              {source === 'history' && history.isPending && (
                <p className="text-gray-400 text-center py-6">{formatMessage('加载中…')}</p>
              )}
              {source === 'history' && history.data && history.data.items.length === 0 && (
                <p className="text-gray-400 text-center py-6">{formatMessage('没有匹配的历史记录')}</p>
              )}
              {source === 'example' && (examples.data ?? []).length === 0 && (
                <p className="text-gray-400 text-center py-6">{formatMessage('此请求还没有保存的示例')}</p>
              )}
              {source === 'example' && (examples.data ?? []).map((example) => (
                <button
                  key={example.id}
                  className="w-full text-left px-4 py-1.5 border-b border-gray-50 hover:bg-blue-50 flex items-center gap-2"
                  onClick={() => pickExample(example)}
                >
                  <span className="flex-1 min-w-0 truncate"><Verbatim value={example.name} /></span>
                  <span className={example.status < 400 ? 'text-green-600' : 'text-red-600'}>{example.status}</span>
                </button>
              ))}
              {source === 'history' && (history.data?.items ?? []).map((item) => (
                <button
                  key={item.id}
                  className="w-full text-left px-4 py-1.5 border-b border-gray-50 hover:bg-blue-50 flex items-center gap-2"
                  onClick={() => void pick(item)}
                >
                  <span className="font-mono text-gray-500 w-12 shrink-0">{item.method}</span>
                  <span className="flex-1 min-w-0 truncate font-mono"><Verbatim value={item.url} /></span>
                  <span className={item.status < 400 ? 'text-green-600' : 'text-red-600'}>{item.status}</span>
                </button>
              ))}
            </div>
          </div>
        )}

        {(picked || pickedExample) && (
          <div className="flex-1 flex flex-col min-h-0">
            <div className="px-4 py-2 border-b flex items-center gap-3 text-xs">
              <button
                className="border rounded px-2 py-0.5 text-gray-600 hover:bg-gray-50"
                onClick={() => {
                  setPicked(null);
                  setPickedExample(null);
                  setOther(null);
                }}
              >
                {formatMessage('← 重新选择')}
              </button>
              <label className="flex items-center gap-1 text-gray-600">
                <input
                  type="checkbox"
                  checked={normalize}
                  onChange={(e) => setNormalize(e.target.checked)}
                />
                {formatMessage('格式归一（JSON 排序键 + 缩进）')}
              </label>
              {loadingSide && <span className="text-gray-400">{formatMessage('加载中…')}</span>}
              {sideError && <span className="text-red-600"><Verbatim value={sideError} /></span>}
            </div>
            <div className="flex-1 overflow-auto font-mono text-xs leading-5">
              {tooLarge && (
                <p className="px-4 py-3 text-orange-700 bg-orange-50">
                  {formatMessage('响应正文过大，无法进行行级对比，请直接在 Body 视图查看两侧内容。')}
                </p>
              )}
              {(rows ?? []).map((row, i) => (
                <div
                  key={i}
                  data-diff={row.type}
                  className={
                    row.type === 'add'
                      ? 'bg-green-50 text-green-800 whitespace-pre-wrap break-all px-4'
                      : row.type === 'del'
                        ? 'bg-red-50 text-red-700 whitespace-pre-wrap break-all px-4'
                        : 'text-gray-600 whitespace-pre-wrap break-all px-4'
                  }
                >
                  {row.type === 'add' ? '+ ' : row.type === 'del' ? '- ' : '  '}
                  <Verbatim value={row.text} />
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </ModalFrame>
  );
}
