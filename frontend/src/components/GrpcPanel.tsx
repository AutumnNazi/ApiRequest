// gRPC 调用面板：连接 → 服务/方法列表 → unary / streaming 调用 → 响应
import { useEffect, useRef, useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { json } from '@codemirror/lang-json';
import {
  grpcDiscover,
  grpcCall,
  grpcStreamOpen,
  grpcStreamSend,
  grpcStreamClose,
  grpcStreamCloseSend,
  onGrpcStream,
  toAppError,
  type GrpcMethodInfo,
  type GrpcCallResult,
  type GrpcStreamMessage,
} from '../ipc';
import { translate, Verbatim, formatMessage } from '../i18n/locale';
import { useRecentTargets } from '../hooks/useRecentTargets';
import RecentTargets from './RecentTargets';
import ModalFrame from './ModalFrame';

interface Props {
  onClose(): void;
}

interface StreamEntry {
  ts: number;
  kind: 'message' | 'error' | 'system' | 'sent';
  data: string;
}

const MAX_STREAM_LOG_ENTRIES = 1000;

// CodeMirror 扩展只需创建一次
const jsonExtensions = [json()];

export default function GrpcPanel({ onClose }: Props) {
  const [target, setTarget] = useState('');
  const [useTls, setUseTls] = useState(false);
  const [methods, setMethods] = useState<GrpcMethodInfo[]>([]);
  const [selected, setSelected] = useState<GrpcMethodInfo | null>(null);
  const [requestJSON, setRequestJSON] = useState('{}');
  const [result, setResult] = useState<GrpcCallResult | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  // 流式状态
  const [streamId, setStreamId] = useState<string>('');
  const [streamLog, setStreamLog] = useState<StreamEntry[]>([]);
  const streamIdRef = useRef('');
  const streamLogRef = useRef<StreamEntry[]>([]);
  const [streamInput, setStreamInput] = useState('');
  const [sendClosed, setSendClosed] = useState(false);
  const { recents, recall } = useRecentTargets('protocol:recent:grpc');

  const pickRecent = (value: string) => {
    setTarget(value);
    setError('');
  };

  const cfg = (resolvedTarget = target) => ({ target: resolvedTarget, useTls });

  const attemptDiscover = async () => {
    const resolvedTarget = target.trim();
    if (!resolvedTarget) return;
    setTarget(resolvedTarget);
    if (await discover(resolvedTarget)) recall(resolvedTarget);
  };

  const appendStreamLog = (entry: StreamEntry) => {
    const next = [...streamLogRef.current, entry].slice(-MAX_STREAM_LOG_ENTRIES);
    streamLogRef.current = next;
    setStreamLog(next);
  };

  const discover = async (resolvedTarget = target): Promise<boolean> => {
    endStream();
    setError('');
    setBusy(true);
    setMethods([]);
    setSelected(null);
    try {
      setMethods(await grpcDiscover(cfg(resolvedTarget)));
      return true;
    } catch (e) {
      setError(toAppError(e).detail);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const pick = (m: GrpcMethodInfo) => {
    endStream();
    streamLogRef.current = [];
    setStreamLog([]);
    setSelected(m);
    setRequestJSON(m.inputExample || '{}');
    setResult(null);
    setError('');
  };

  const isStreaming = (m: GrpcMethodInfo | null) =>
    !!m && (m.clientStream || m.serverStream);

  // 订阅 grpc:stream 事件（仅关心当前会话）
  useEffect(() => {
    const unsub = onGrpcStream((m: GrpcStreamMessage) => {
      if (m.streamId !== streamIdRef.current) return;
      const kind: StreamEntry['kind'] = m.kind === 'done' ? 'system' : m.kind;
      const entry: StreamEntry = {
        ts: m.ts,
        kind,
        data: m.data || (m.kind === 'done' ? '流已完成' : ''),
      };
      appendStreamLog(entry);
      if (m.kind === 'done' || m.kind === 'error') {
        streamIdRef.current = '';
        setStreamId('');
        setSendClosed(true);
      }
    });
    return unsub;
  }, []);

  const invoke = async () => {
    if (!selected) return;
    setError('');
    setBusy(true);
    setResult(null);
    try {
      setResult(await grpcCall(cfg(), selected.fullName, requestJSON));
    } catch (e) {
      setError(toAppError(e).detail);
    } finally {
      setBusy(false);
    }
  };

  // 启动流（server-stream/bidi）：用一个稳定 sessionId
  const openStream = async () => {
    if (!selected) return;
    setError('');
    endStream();
    setBusy(true);
    setSendClosed(false);
    const id = 'grpc-' + Date.now() + '-' + newStreamSuffix();
    streamIdRef.current = id;
    streamLogRef.current = [{ ts: Date.now(), kind: 'system', data: '流已打开' }];
    setStreamLog(streamLogRef.current);
    setStreamId(id);
    try {
      await grpcStreamOpen(id, cfg(), selected.fullName, {});
      // 四种 RPC 都需要至少一条 request message。server-stream 的请求侧是 unary，
      // 发完唯一消息后立即半关；client-stream/bidi 则保留发送侧供用户追加。
      if (!(await sendStream(requestJSON, id))) {
        await grpcStreamClose(id).catch(() => {});
        streamIdRef.current = '';
        setStreamId('');
        setSendClosed(true);
        return;
      }
      if (!selected.clientStream) {
        if (!(await closeSend(id, '请求已发送；等待服务端流式响应'))) {
          await grpcStreamClose(id).catch(() => {});
          streamIdRef.current = '';
          setStreamId('');
          setSendClosed(true);
        }
      }
    } catch (e) {
      await grpcStreamClose(id).catch(() => {});
      setError(toAppError(e).detail);
      streamIdRef.current = '';
      setStreamId('');
      streamLogRef.current = [
        ...streamLogRef.current,
        { ts: Date.now(), kind: 'error', data: toAppError(e).detail },
      ];
      setStreamLog(streamLogRef.current);
      setSendClosed(true);
    } finally {
      setBusy(false);
    }
  };

  const sendStream = async (payload?: string, targetId?: string) => {
    const id = targetId ?? streamIdRef.current;
    if (!id) return false;
    const body = payload ?? streamInput;
    if (!body.trim()) return false;
    try {
      await grpcStreamSend(id, body);
    appendStreamLog({ ts: Date.now(), kind: 'sent', data: body });
      if (payload === undefined) setStreamInput('');
      return true;
    } catch (e) {
      const detail = toAppError(e).detail;
      setError(detail);
      appendStreamLog({ ts: Date.now(), kind: 'error', data: detail });
      return false;
    }
  };

  const closeSend = async (targetId?: string, message = '已完成发送；等待服务端收尾') => {
    const id = targetId ?? streamIdRef.current;
    // 后端 CloseSend 已做幂等；不要读取旧 render 闭包里的 sendClosed，
    // 否则上一条流结束后重新打开 server-stream 会被误判为已半关。
    if (!id) return false;
    try {
      await grpcStreamCloseSend(id);
      setSendClosed(true);
      appendStreamLog({ ts: Date.now(), kind: 'system', data: message });
      return true;
    } catch (e) {
      const detail = toAppError(e).detail;
      setError(detail);
      appendStreamLog({ ts: Date.now(), kind: 'error', data: detail });
      return false;
    }
  };

  const endStream = () => {
    const id = streamIdRef.current;
    if (!id) return;
    grpcStreamClose(id).catch(() => {});
    streamIdRef.current = '';
    setStreamId('');
    setSendClosed(true);
    appendStreamLog({ ts: Date.now(), kind: 'system', data: '流已关闭' });
  };

  // 卸载时关闭流
  useEffect(() => {
    return () => {
      const id = streamIdRef.current;
      if (id) grpcStreamClose(id).catch(() => {});
    };
  }, []);

  const streaming = isStreaming(selected);
  const showStreamLog = streaming && (streamId !== '' || streamLog.length > 0);

  return (
    <ModalFrame
      onClose={onClose}
      titleId="grpc-panel-title"
      className="bg-white rounded-lg shadow-xl w-[900px] h-[640px] flex flex-col"
    >
        {/* 连接行 */}
        <div className="flex items-center gap-2 px-4 py-3 border-b">
          <h2 id="grpc-panel-title" className="font-semibold text-sm shrink-0">gRPC</h2>
          <input
            className="flex-1 border rounded px-2 py-1 text-sm font-mono"
            placeholder="localhost:50051"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && target && attemptDiscover()}
          />
          <label className="flex items-center gap-1 text-xs text-gray-600">
            <input type="checkbox" checked={useTls} onChange={(e) => setUseTls(e.target.checked)} />
            TLS
          </label>
          <button
            className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
            disabled={!target.trim() || busy}
            onClick={attemptDiscover}
          >
            {busy && methods.length === 0 ? formatMessage('发现中…') : formatMessage('发现服务')}
          </button>
          <button className="text-gray-400 hover:text-gray-700 ml-1" onClick={onClose}>
            ×
          </button>
        </div>

        {error && <div className="px-4 py-2 bg-red-50 border-b text-xs text-red-600"><Verbatim value={error} /></div>}

        <RecentTargets recents={recents} current={target} onPick={pickRecent} />

        <div className="flex-1 flex min-h-0">
          {/* 方法列表 */}
          <div className="w-72 border-r overflow-auto">
            {methods.length === 0 ? (
              <div className="h-full flex items-center justify-center text-gray-400 text-xs px-6 text-center">
                {formatMessage('输入地址并"发现服务"（目标需开启 server reflection）')}
              </div>
            ) : (
              methods.map((m) => (
                <div
                  key={m.fullName}
                  className={`px-3 py-1.5 text-xs cursor-pointer border-b border-gray-50 ${
                    selected?.fullName === m.fullName ? 'bg-blue-50 text-blue-700' : 'hover:bg-gray-50'
                  }`}
                  onClick={() => pick(m)}
                >
                  <div className="font-mono truncate"><Verbatim value={m.method} /></div>
                  <div className="text-gray-400 truncate"><Verbatim value={m.service} /></div>
                  {(m.clientStream || m.serverStream) && (
                    <span className="text-purple-600">
                      {m.clientStream && m.serverStream ? formatMessage('bidi 流')
                        : m.clientStream ? formatMessage('client 流')
                        : formatMessage('server 流')}
                    </span>
                  )}
                </div>
              ))
            )}
          </div>

          {/* 请求 / 响应 */}
          <div className="flex-1 flex flex-col min-w-0">
            {selected ? (
              <>
                <div className="flex items-center gap-2 px-3 py-2 border-b">
                  <span className="font-mono text-xs text-gray-600 truncate flex-1">
                    <Verbatim value={selected.fullName} />
                  </span>
                  {streaming && streamId ? (
                    <>
                      <button
                        className="bg-green-600 text-white rounded px-4 py-1 text-sm hover:bg-green-700 disabled:opacity-50"
                        disabled={busy || !selected.clientStream || sendClosed || !streamInput.trim()}
                        onClick={() => sendStream()}
                      >
                        {formatMessage('发送')}
                      </button>
                      <button
                        className="bg-red-600 text-white rounded px-4 py-1 text-sm hover:bg-red-700"
                        onClick={endStream}
                      >
                        {formatMessage('结束流')}
                      </button>
                      {selected.clientStream && !sendClosed && (
                        <button
                          className="bg-amber-600 text-white rounded px-4 py-1 text-sm hover:bg-amber-700"
                          onClick={() => closeSend()}
                          title={formatMessage('向服务端半关闭发送方向（流仍开放接收）')}
                        >
                          {selected.serverStream ? formatMessage('半关发送') : formatMessage('完成发送')}
                        </button>
                      )}
                    </>
                  ) : streaming ? (
                    <button
                      className="bg-blue-600 text-white rounded px-4 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
                      disabled={busy}
                      onClick={openStream}
                    >
                      {busy ? formatMessage('连接中…') : formatMessage('开始流')}
                    </button>
                  ) : (
                    <button
                      className="bg-blue-600 text-white rounded px-4 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
                      disabled={busy}
                      onClick={invoke}
                    >
                      {busy ? formatMessage('调用中…') : formatMessage('调用')}
                    </button>
                  )}
                </div>

                {/* 单条 unary 的请求编辑 + 单次响应 */}
                {showStreamLog ? (
                  // 流式视图：下方流日志 + 输入框
                  <div className="flex-1 flex flex-col min-h-0">
                    <div className="px-3 py-1 text-xs text-gray-500 bg-gray-50">{formatMessage('流消息（按时间排序）')}</div>
                    <div className="flex-1 overflow-auto p-3 text-xs font-mono whitespace-pre-wrap break-all">
                      {streamLog.length === 0 ? (
                        <span className="text-gray-400">{formatMessage('暂无消息')}</span>
                      ) : (
                        streamLog.map((it, i) => (
                          <div key={i} className={`mb-2 ${colorByKind(it.kind)}`}>
                            <div className="text-gray-400 text-[10px]">
                              {new Date(it.ts).toLocaleTimeString()} · <Verbatim value={it.kind} />
                            </div>
                            <pre className="whitespace-pre-wrap break-all">
                              {it.kind === 'system' ? translate(it.data) : <Verbatim value={it.data} />}
                            </pre>
                          </div>
                        ))
                      )}
                    </div>
                    {selected.clientStream && streamId && !sendClosed && (
                      <div className="border-t p-2 flex gap-2">
                        <input
                          className="flex-1 border rounded px-2 py-1 text-xs font-mono"
                          placeholder={
                            selected.serverStream
                              ? formatMessage('输入一条 JSON 消息后回车发送…')
                              : formatMessage('输入一条 JSON 消息后回车发送…')
                          }
                          value={streamInput}
                          onChange={(e) => setStreamInput(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') sendStream();
                          }}
                        />
                        <button
                          className="bg-green-600 text-white rounded px-3 py-1 text-xs hover:bg-green-700"
                          onClick={() => sendStream()}
                        >
                          {formatMessage('发送')}
                        </button>
                      </div>
                    )}
                  </div>
                ) : (
                  <>
                    <div className="h-2/5 border-b flex flex-col min-h-0">
                      <div className="px-3 py-1 text-xs text-gray-500 bg-gray-50">{formatMessage('请求（JSON）')}</div>
                      <div className="flex-1 overflow-auto">
                        <CodeMirror
                          height="100%"
                          value={requestJSON}
                          extensions={jsonExtensions}
                          onChange={setRequestJSON}
                        />
                      </div>
                    </div>
                    <div className="h-3/5 flex flex-col min-h-0">
                      <div className="px-3 py-1 text-xs text-gray-500 bg-gray-50 flex items-center gap-3">
                        {formatMessage('响应')}
                        {result && <span className="text-gray-400">{result.durationMs} ms</span>}
                      </div>
                      <pre className="flex-1 overflow-auto p-3 text-xs font-mono whitespace-pre-wrap break-all">
                        {result ? <Verbatim value={result.response} /> : formatMessage('调用后在此显示响应')}
                      </pre>
                    </div>
                  </>
                )}
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center text-gray-400 text-sm">
                {formatMessage('从左侧选择一个方法')}
              </div>
            )}
          </div>
        </div>
    </ModalFrame>
  );
}

function colorByKind(kind: StreamEntry['kind']): string {
  switch (kind) {
    case 'message':
      return 'text-gray-800';
    case 'sent':
      return 'text-blue-700';
    case 'error':
      return 'text-red-600';
    default:
      return 'text-green-700';
  }
}

function newStreamSuffix(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return Math.random().toString(36).slice(2);
}
