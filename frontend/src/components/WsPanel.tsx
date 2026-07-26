// WebSocket / SSE 会话面板：连接 + 消息时间线 + 发送框
import { useEffect, useRef, useState } from 'react';
import {
  openSession,
  sendSessionMessage,
  closeSession,
  onProtoMessage,
  toAppError,
  type InboundMsg,
} from '../ipc';

interface Props {
  onClose(): void;
}

export default function WsPanel({ onClose }: Props) {
  const [protocol, setProtocol] = useState<'websocket' | 'sse'>('websocket');
  const [url, setUrl] = useState('');
  const [connected, setConnected] = useState(false);
  const [messages, setMessages] = useState<InboundMsg[]>([]);
  const [outgoing, setOutgoing] = useState('');
  const [error, setError] = useState('');
  const sessionIdRef = useRef('');
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => onProtoMessage((m) => {
    if (m.sessionId !== sessionIdRef.current) return;
    setMessages((prev) => [...prev, m]);
    if (m.kind === 'close' || m.kind === 'error') setConnected(false);
  }), []);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // 组件卸载时断开
  useEffect(() => () => {
    if (sessionIdRef.current) closeSession(sessionIdRef.current);
  }, []);

  const connect = async () => {
    setError('');
    sessionIdRef.current = `ws-${Date.now()}`;
    try {
      await openSession(sessionIdRef.current, { protocol, url });
      setConnected(true);
    } catch (e) {
      setError(toAppError(e).detail);
    }
  };

  const disconnect = async () => {
    await closeSession(sessionIdRef.current);
    setConnected(false);
  };

  const send = async () => {
    if (!outgoing.trim()) return;
    try {
      await sendSessionMessage(sessionIdRef.current, outgoing);
      setOutgoing('');
    } catch (e) {
      setError(toAppError(e).detail);
    }
  };

  const dirStyle: Record<string, string> = {
    in: 'bg-blue-50 text-blue-900',
    out: 'bg-green-50 text-green-900 ml-auto',
    system: 'bg-gray-100 text-gray-500 mx-auto text-center',
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[720px] h-[560px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b">
          <select
            className="border rounded px-2 py-1 text-xs"
            value={protocol}
            onChange={(e) => setProtocol(e.target.value as 'websocket' | 'sse')}
            disabled={connected}
          >
            <option value="websocket">WebSocket</option>
            <option value="sse">SSE</option>
          </select>
          <input
            className="flex-1 border rounded px-2 py-1 text-sm font-mono"
            placeholder={protocol === 'websocket' ? 'wss://echo.websocket.org' : 'https://example.com/events'}
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            disabled={connected}
            onKeyDown={(e) => e.key === 'Enter' && !connected && url && connect()}
          />
          <button
            className={`text-sm rounded px-4 py-1 ${
              connected
                ? 'border border-red-200 text-red-500 hover:bg-red-50'
                : 'bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50'
            }`}
            disabled={!connected && !url.trim()}
            onClick={connected ? disconnect : connect}
          >
            {connected ? '断开' : '连接'}
          </button>
          <button className="text-gray-400 hover:text-gray-700 ml-1" onClick={onClose}>
            ×
          </button>
        </div>

        {error && <div className="px-4 py-2 bg-red-50 border-b text-xs text-red-600">{error}</div>}

        {/* 消息时间线 */}
        <div className="flex-1 overflow-auto p-3 space-y-1.5 flex flex-col">
          {messages.length === 0 && (
            <div className="m-auto text-gray-400 text-sm">连接后消息将显示在此处</div>
          )}
          {messages.map((m, i) => (
            <div
              key={i}
              className={`max-w-[80%] rounded px-2.5 py-1.5 text-xs font-mono whitespace-pre-wrap break-all ${
                dirStyle[m.direction] ?? ''
              }`}
            >
              {m.direction === 'system' ? (
                <span>
                  ● {m.kind} {m.data && `· ${m.data}`}
                </span>
              ) : (
                <>
                  {m.event && <span className="text-purple-600">[{m.event}] </span>}
                  {m.data}
                </>
              )}
            </div>
          ))}
          <div ref={bottomRef} />
        </div>

        {/* 发送框（仅 WS） */}
        {protocol === 'websocket' && (
          <div className="flex gap-2 p-3 border-t">
            <textarea
              className="flex-1 border rounded px-2 py-1.5 text-sm font-mono resize-none h-16"
              placeholder="输入消息，Ctrl+Enter 发送"
              value={outgoing}
              onChange={(e) => setOutgoing(e.target.value)}
              disabled={!connected}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) send();
              }}
            />
            <button
              className="bg-blue-600 text-white rounded px-4 text-sm hover:bg-blue-700 disabled:opacity-50"
              disabled={!connected || !outgoing.trim()}
              onClick={send}
            >
              发送
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
