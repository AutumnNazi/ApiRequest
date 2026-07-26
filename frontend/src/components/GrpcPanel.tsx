// gRPC 调用面板：连接 → 服务/方法列表 → JSON 请求编辑 → unary 调用 → 响应
import { useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { json } from '@codemirror/lang-json';
import {
  grpcDiscover,
  grpcCall,
  toAppError,
  type GrpcMethodInfo,
  type GrpcCallResult,
} from '../ipc';

interface Props {
  onClose(): void;
}

export default function GrpcPanel({ onClose }: Props) {
  const [target, setTarget] = useState('');
  const [useTls, setUseTls] = useState(false);
  const [methods, setMethods] = useState<GrpcMethodInfo[]>([]);
  const [selected, setSelected] = useState<GrpcMethodInfo | null>(null);
  const [requestJSON, setRequestJSON] = useState('{}');
  const [result, setResult] = useState<GrpcCallResult | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const cfg = () => ({ target, useTls });

  const discover = async () => {
    setError('');
    setBusy(true);
    setMethods([]);
    setSelected(null);
    try {
      setMethods(await grpcDiscover(cfg()));
    } catch (e) {
      setError(toAppError(e).detail);
    } finally {
      setBusy(false);
    }
  };

  const pick = (m: GrpcMethodInfo) => {
    setSelected(m);
    setRequestJSON(m.inputExample || '{}');
    setResult(null);
    setError('');
  };

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

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[860px] h-[600px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 连接行 */}
        <div className="flex items-center gap-2 px-4 py-3 border-b">
          <h2 className="font-semibold text-sm shrink-0">gRPC</h2>
          <input
            className="flex-1 border rounded px-2 py-1 text-sm font-mono"
            placeholder="localhost:50051"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && target && discover()}
          />
          <label className="flex items-center gap-1 text-xs text-gray-600">
            <input type="checkbox" checked={useTls} onChange={(e) => setUseTls(e.target.checked)} />
            TLS
          </label>
          <button
            className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
            disabled={!target.trim() || busy}
            onClick={discover}
          >
            {busy && methods.length === 0 ? '发现中…' : '发现服务'}
          </button>
          <button className="text-gray-400 hover:text-gray-700 ml-1" onClick={onClose}>
            ×
          </button>
        </div>

        {error && <div className="px-4 py-2 bg-red-50 border-b text-xs text-red-600">{error}</div>}

        <div className="flex-1 flex min-h-0">
          {/* 方法列表 */}
          <div className="w-72 border-r overflow-auto">
            {methods.length === 0 ? (
              <div className="h-full flex items-center justify-center text-gray-400 text-xs px-6 text-center">
                输入地址并"发现服务"（目标需开启 server reflection）
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
                  <div className="font-mono truncate">{m.method}</div>
                  <div className="text-gray-400 truncate">{m.service}</div>
                  {(m.clientStream || m.serverStream) && (
                    <span className="text-yellow-600">流式（暂不支持调用）</span>
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
                    {selected.fullName}
                  </span>
                  <button
                    className="bg-blue-600 text-white rounded px-4 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
                    disabled={busy || selected.clientStream || selected.serverStream}
                    onClick={invoke}
                  >
                    {busy ? '调用中…' : '调用'}
                  </button>
                </div>
                <div className="flex-1 flex flex-col min-h-0">
                  <div className="h-1/2 border-b flex flex-col min-h-0">
                    <div className="px-3 py-1 text-xs text-gray-500 bg-gray-50">请求（JSON）</div>
                    <div className="flex-1 overflow-auto">
                      <CodeMirror
                        height="100%"
                        value={requestJSON}
                        extensions={[json()]}
                        onChange={setRequestJSON}
                      />
                    </div>
                  </div>
                  <div className="h-1/2 flex flex-col min-h-0">
                    <div className="px-3 py-1 text-xs text-gray-500 bg-gray-50 flex items-center gap-3">
                      响应
                      {result && <span className="text-gray-400">{result.durationMs} ms</span>}
                    </div>
                    <pre className="flex-1 overflow-auto p-3 text-xs font-mono whitespace-pre-wrap break-all">
                      {result?.response ?? '调用后在此显示响应'}
                    </pre>
                  </div>
                </div>
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center text-gray-400 text-sm">
                从左侧选择一个方法
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
