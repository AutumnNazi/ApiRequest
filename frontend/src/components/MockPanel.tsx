// Mock Server 面板：启停 + 地址 + 请求日志
import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  startMockServer,
  stopMockServer,
  runningMocks,
  onMockLog,
  listCollectionExamples,
  upsertExample,
  toAppError,
  type MockLogEntry,
  type Example,
} from '../ipc';
import { Verbatim, formatMessage } from '../i18n/locale';
import ModalFrame from './ModalFrame';

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
  // 初始查询未落地前禁用启停：否则先完成的启动结果会被随后到达的旧快照覆盖，
  // UI 显示"已停止"而服务仍在运行，再次点击会重启并换掉端口。
  const [initializing, setInitializing] = useState(true);
  // 启停操作序号：只有最新一轮异步操作才允许回写状态。
  const revision = useRef(0);
  const mounted = useRef(false);
  const qc = useQueryClient();
  const [showScripts, setShowScripts] = useState(false);
  const [expandedId, setExpandedId] = useState('');
  const [draft, setDraft] = useState('');
  const examplesQuery = useQuery({
    queryKey: ['mock-examples', collectionId],
    queryFn: () => listCollectionExamples(collectionId),
    enabled: showScripts,
  });
  const saveScript = useMutation({
    mutationFn: (e: Example) => upsertExample({ ...e, mockScript: draft }),
    onSuccess: () => {
      setExpandedId('');
      qc.invalidateQueries({ queryKey: ['mock-examples', collectionId] });
    },
    onError: (cause) => {
      if (mounted.current) setError(toAppError(cause).detail);
    },
  });

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      revision.current += 1;
    };
  }, []);

  useEffect(() => {
    let active = true;
    // 集合切换会使上一集合仍在飞行中的启停操作失效；本 effect 自身由 active 保护。
    revision.current += 1;
    // 这些状态都属于当前集合。切换集合时不能继续展示上一集合的操作状态或日志。
    setAddr('');
    setError('');
    setLogs([]);
    setBusy(false);
    setInitializing(true);
    runningMocks()
      .then((m) => {
        if (active && mounted.current) {
          setAddr(m[collectionId] ?? '');
        }
      })
      // 查询失败不应静默：否则面板显示"未启动"，用户误以为可以重复启动
      .catch((cause) => {
        if (active && mounted.current) {
          setError(toAppError(cause).detail);
        }
      })
      .finally(() => {
        if (active && mounted.current) {
          setInitializing(false);
        }
      });
    return () => {
      active = false;
    };
  }, [collectionId]);

  useEffect(() => {
    const unsub = onMockLog((entry) => {
      if (entry.collectionId === collectionId) {
        setLogs((prev) => [entry, ...prev].slice(0, 200));
      }
    });
    return unsub;
  }, [collectionId]);

  const toggle = async () => {
    setError('');
    setBusy(true);
    // 让仍在飞行中的 runningMocks 快照失效，避免它回写过期状态
    const issued = ++revision.current;
    try {
      if (addr) {
        await stopMockServer(collectionId);
        if (mounted.current && issued === revision.current) setAddr('');
      } else {
        const status = await startMockServer(collectionId);
        if (mounted.current && issued === revision.current) setAddr(status.addr);
      }
    } catch (e) {
      if (mounted.current && issued === revision.current) {
        setError(toAppError(e).detail);
      }
    } finally {
      if (mounted.current && issued === revision.current) setBusy(false);
    }
  };

  return (
    <ModalFrame
      onClose={onClose}
      titleId="mock-panel-title"
      className="bg-white rounded-lg shadow-xl w-[640px] h-[460px] flex flex-col"
    >
        <div className="flex items-center gap-3 px-4 py-3 border-b">
          <h2 id="mock-panel-title" className="font-semibold text-sm">Mock · <Verbatim value={collectionName} /></h2>
          <span
            className={`w-2 h-2 rounded-full ${addr ? 'bg-green-500' : 'bg-gray-300'}`}
            title={addr ? formatMessage('运行中') : formatMessage('已停止')}
          />
          <button
            className={`text-sm rounded px-3 py-1 border ${
              showScripts ? 'border-blue-300 bg-blue-50 text-blue-700' : 'text-gray-500 hover:bg-gray-100'
            }`}
            title={formatMessage('按请求动态生成响应的 JS 脚本')}
            onClick={() => setShowScripts((v) => !v)}
          >
            {formatMessage('响应脚本')}
          </button>
          <button
            className={`ml-auto text-sm rounded px-4 py-1 ${
              addr
                ? 'border border-red-200 text-red-500 hover:bg-red-50'
                : 'bg-blue-600 text-white hover:bg-blue-700'
            } disabled:opacity-50`}
            disabled={busy || initializing}
            onClick={toggle}
          >
            {addr ? formatMessage('停止') : formatMessage('启动')}
          </button>
          <button className="text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>

        {addr && (
          <div className="flex items-center gap-2 px-4 py-2 bg-green-50 border-b text-sm">
            <span className="font-mono text-green-800"><Verbatim value={addr} /></span>
            <button
              className="text-xs text-green-700 hover:underline"
              onClick={() => navigator.clipboard.writeText(addr)}
            >
              {formatMessage('复制')}
            </button>
          </div>
        )}
        {error && (
          <div className="px-4 py-2 bg-red-50 border-b text-xs text-red-600"><Verbatim value={error} /></div>
        )}

        {showScripts && (
          <div className="flex-1 overflow-auto text-xs">
            {examplesQuery.isPending && (
              <p className="text-gray-400 text-center py-6">{formatMessage('加载中…')}</p>
            )}
            {examplesQuery.data && examplesQuery.data.length === 0 && (
              <p className="text-gray-400 text-center py-6">
                {formatMessage('此集合还没有示例；在响应区"保存为示例"后即可在此配置脚本')}
              </p>
            )}
            {(examplesQuery.data ?? []).map((e) => (
              <div key={e.id} className="border-b border-gray-100">
                <button
                  className="w-full text-left px-4 py-2 flex items-center gap-2 hover:bg-gray-50"
                  onClick={() => {
                    if (expandedId === e.id) {
                      setExpandedId('');
                    } else {
                      setExpandedId(e.id);
                      setDraft(e.mockScript ?? '');
                    }
                  }}
                >
                  <span className="font-medium">{expandedId === e.id ? '▾' : '▸'}</span>
                  <span className="flex-1 min-w-0 truncate"><Verbatim value={e.name} /></span>
                  {e.mockScript && <span className="text-blue-600">{formatMessage('已配置')}</span>}
                  <span className="text-gray-400">{e.status}</span>
                </button>
                {expandedId === e.id && (
                  <div className="px-4 pb-3 space-y-2">
                    <textarea
                      className="w-full h-32 border rounded p-2 font-mono text-xs outline-none focus:border-blue-400"
                      placeholder={formatMessage(
                        'respond({ status: 200, headers: {...}, body: "..." , delayMs: 0 })；不调用则回退静态响应',
                      )}
                      value={draft}
                      onChange={(ev) => setDraft(ev.target.value)}
                    />
                    <div className="flex items-center gap-2">
                      <button
                        className="bg-blue-600 text-white rounded px-3 py-1 hover:bg-blue-700"
                        disabled={saveScript.isPending}
                        onClick={() => saveScript.mutate(e)}
                      >
                        {formatMessage('保存')}
                      </button>
                      <span className="text-gray-400">
                        {formatMessage('脚本上下文：mockRequest（method/path/query/queryFirst/headers/body）')}
                      </span>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {!showScripts && (
        <div className="flex-1 overflow-auto">
          {logs.length === 0 ? (
            <div className="h-full flex items-center justify-center text-gray-400 text-sm px-8 text-center">
              {addr
                ? formatMessage('等待请求…向上方地址发请求即返回对应示例')
                : formatMessage('启动后，此集合中带"示例"的请求会按路径/方法提供 mock 响应')}
            </div>
          ) : (
            <table className="w-full text-xs">
              <tbody>
                {logs.map((l, i) => (
                  <tr key={i} className="border-b border-gray-100">
                    <td className="p-2 font-semibold w-16"><Verbatim value={l.method} /></td>
                    <td className="p-2 font-mono"><Verbatim value={l.path} /></td>
                    <td className="p-2 text-gray-400"><Verbatim value={l.matched || '—'} /></td>
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
        )}
    </ModalFrame>
  );
}
