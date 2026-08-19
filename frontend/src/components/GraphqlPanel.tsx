// GraphQL 内省面板：endpoint URL → 内省 → 展示 Queries/Mutations 列表 + schema JSON 预览
import { useState } from 'react';
import {
  graphqlIntrospect,
  toAppError,
  type GraphqlResult,
} from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useRecentTargets } from '../hooks/useRecentTargets';
import RecentTargets from './RecentTargets';
import { parseHeaderLine } from '../utils/request';
import { buildGraphqlOperation, type GraphqlOperationField } from '../utils/graphql';

interface Props {
  onClose(): void;
  /** 点击内省出的操作时回调：携带生成好的 GraphQL 请求（url / query / variables），由调用方新建标签 */
  onOpenRequest?(req: { url: string; query: string; variables: string; authHeader?: { key: string; value: string } }): void;
}

const SCHEMA_RENDER_CHAR_LIMIT = 500_000;

export default function GraphqlPanel({ onClose, onOpenRequest }: Props) {
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<GraphqlResult | null>(null);
  const [authHeader, setAuthHeader] = useState('');
  const { recents, recall } = useRecentTargets('protocol:recent:graphql');

  const pickRecent = (value: string) => {
    setUrl(value);
    setError('');
  };

  const attemptDiscover = async () => {
    const resolvedUrl = url.trim();
    if (!resolvedUrl) return;
    setUrl(resolvedUrl);
    if (await discover(resolvedUrl)) recall(resolvedUrl);
  };

  // 由内省出的操作生成 GraphQL 请求模板（query/mutation + 变量定义），交给调用方新建标签。
  const openRequest = (field: GraphqlOperationField) => {
    if (!onOpenRequest) return;
    const { query, variables } = buildGraphqlOperation(field);
    onOpenRequest({
      url: url.trim(),
      query,
      variables,
      authHeader: parseHeaderLine(authHeader) ?? undefined,
    });
    onClose();
  };

  const discover = async (resolvedUrl = url.trim()): Promise<boolean> => {
    if (!resolvedUrl) return false;
    setBusy(true);
    setError('');
    setResult(null);
    try {
      const headers: Record<string, string> = {};
      if (authHeader.trim()) {
        // 形如 "Authorization: Bearer xxx" 的整行
        const i = authHeader.indexOf(':');
        if (i > 0) {
          headers[authHeader.slice(0, i).trim()] = authHeader.slice(i + 1).trim();
        }
      }
      const res = await graphqlIntrospect({ url: resolvedUrl, headers });
      setResult(res);
      return true;
    } catch (e) {
      const ae = toAppError(e);
      // 后端默认 20s 超时，区分"超时" vs 一般网络错（后端 detail 含 context deadline / timeout 关键字）
      const isTimeout = /deadline exceeded|timeout/i.test(ae.detail);
      setError(
        isTimeout
          ? formatMessage('请求超时（后端默认 20s）：{detail}', { detail: ae.detail })
          : ae.detail,
      );
      return false;
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="graphql-panel-title"
        className="bg-white rounded-lg shadow-xl w-[860px] h-[600px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b">
          <h2 id="graphql-panel-title" className="font-semibold text-sm shrink-0">{formatMessage('GraphQL Schema 内省')}</h2>
          <input
            className="flex-1 border rounded px-2 py-1 text-sm font-mono"
            placeholder="https://api.example.com/graphql"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && url && attemptDiscover()}
          />
          <input
            className="w-64 border rounded px-2 py-1 text-xs font-mono"
            placeholder="Authorization: Bearer xxx"
            value={authHeader}
            onChange={(e) => setAuthHeader(e.target.value)}
          />
          <button
            className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
            disabled={!url.trim() || busy}
            onClick={attemptDiscover}
          >
            {busy ? formatMessage('内省中…') : formatMessage('内省 Schema')}
          </button>
          <button className="text-gray-400 hover:text-gray-700 ml-1" onClick={onClose}>
            ×
          </button>
        </div>

        {error && <div className="px-4 py-2 bg-red-50 border-b text-xs text-red-600"><Verbatim value={error} /></div>}

        <RecentTargets recents={recents} current={url} onPick={pickRecent} />

        <div className="flex-1 flex min-h-0">
          {/* 操作列表 */}
          <div className="w-72 border-r overflow-auto">
            {!result ? (
              <div className="h-full flex items-center justify-center text-gray-400 text-xs px-6 text-center">
                {formatMessage('输入 GraphQL endpoint（支持 Authorization header）然后点击"内省 Schema"')}
              </div>
            ) : (
              <div className="text-xs">
                <Section title="Queries" items={result.queries} onOpen={openRequest} />
                <Section title="Mutations" items={result.mutations} onOpen={openRequest} />
                {result.subscriptions && result.subscriptions.length > 0 && (
                  <Section title="Subscriptions" items={result.subscriptions} onOpen={openRequest} />
                )}
              </div>
            )}
          </div>

          {/* Schema JSON 预览 */}
          <div className="flex-1 flex flex-col min-w-0">
            <div className="px-3 py-1 text-xs text-gray-500 bg-gray-50 border-b">{formatMessage('Schema JSON（可复制给编辑器/graphql-language-server）')}</div>
            <div className="flex-1 overflow-auto">
              {result && result.schemaJson.length > SCHEMA_RENDER_CHAR_LIMIT && (
                <div className="m-3 mb-0 border border-orange-200 bg-orange-50 rounded px-3 py-2 text-xs text-orange-800">
                  {formatMessage('Schema 过大，仅显示前 ')}{SCHEMA_RENDER_CHAR_LIMIT.toLocaleString()}{formatMessage(' 个字符。')}
                </div>
              )}
              <pre className="p-3 text-xs font-mono whitespace-pre-wrap break-all">
                {result
                  ? <Verbatim value={result.schemaJson.slice(0, SCHEMA_RENDER_CHAR_LIMIT)} />
                  : formatMessage('内省后将显示 schema JSON')}
              </pre>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function Section({
  title,
  items,
  onOpen,
}: {
  title: string;
  items: { name: string; returnType: string; returnKind?: string; description?: string; args?: string }[];
  onOpen?: (field: GraphqlOperationField) => void;
}) {
  if (!items || items.length === 0) return null;
  return (
    <div className="border-b">
      <div className="px-3 py-1 font-semibold bg-gray-50">{title} ({items.length})</div>
      {items.map((f, i) => (
        <div key={i} className="px-3 py-1.5 border-t border-gray-50 hover:bg-gray-50 group">
          <div className="flex items-center gap-2">
            <div className="font-mono text-purple-700 min-w-0">
              <Verbatim value={f.name} />
              <span className="text-gray-400"> : <Verbatim value={f.returnType} /></span>
            </div>
            {onOpen && (
              <button
                className="ml-auto shrink-0 rounded border border-blue-200 px-1.5 py-0.5 text-[10px] text-blue-600 opacity-0 group-hover:opacity-100 hover:bg-blue-50"
                onClick={() =>
                  onOpen({
                    name: f.name,
                    args: f.args,
                    returnKind: f.returnKind,
                    kind: title.toLowerCase().startsWith('mutation')
                      ? 'mutation'
                      : title.toLowerCase().startsWith('subscription')
                        ? 'subscription'
                        : 'query',
                  })
                }
              >
                {formatMessage('打开请求')}
              </button>
            )}
          </div>
          {f.description && (
            <div className="text-gray-500 text-[11px] mt-0.5"><Verbatim value={f.description} /></div>
          )}
          {f.args && f.args !== 'null' && (
            <div className="text-gray-400 text-[10px] mt-0.5">args: <Verbatim value={f.args} /></div>
          )}
        </div>
      ))}
    </div>
  );
}
