// 请求编辑器：method + URL + 发送，下方 Params/Headers/Body 页签
import { useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { json } from '@codemirror/lang-json';
import { javascript } from '@codemirror/lang-javascript';
import KVTable from './KVTable';
import AuthEditor from './AuthEditor';
import CodegenDialog from './CodegenDialog';
import VarPreview, { useActiveVariables } from './VarPreview';
import type { Tab } from '../stores/tabs';
import { useTabs } from '../stores/tabs';
import type { Body, KV } from '../ipc';

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];

const methodColor: Record<string, string> = {
  GET: 'text-green-600',
  POST: 'text-yellow-600',
  PUT: 'text-blue-600',
  PATCH: 'text-purple-600',
  DELETE: 'text-red-600',
};

interface Props {
  tab: Tab;
  workspaceId: string;
  onSend(): void;
  onSave(): void;
}

export default function RequestEditor({ tab, workspaceId, onSend, onSave }: Props) {
  const patchDraft = useTabs((s) => s.patchDraft);
  const [pane, setPane] = useState<'params' | 'headers' | 'body' | 'auth' | 'scripts' | 'settings'>('params');
  const [scriptPhase, setScriptPhase] = useState<'pre' | 'test'>('pre');
  const [showCodegen, setShowCodegen] = useState(false);
  const d = tab.draft;
  const activeVars = useActiveVariables(workspaceId);

  const patchBody = (patch: Partial<Body>) =>
    patchDraft(tab.id, { body: { ...d.body, ...patch } as Body });

  return (
    <div className="flex flex-col h-full">
      {/* method + url + 按钮行 */}
      <div className="flex gap-2 p-3 border-b">
        <select
          className={`border rounded px-2 py-1.5 font-semibold text-sm ${methodColor[d.method] ?? ''}`}
          value={d.method}
          onChange={(e) => patchDraft(tab.id, { method: e.target.value })}
        >
          {METHODS.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
        <input
          className="flex-1 border rounded px-3 py-1.5 text-sm font-mono outline-none focus:border-blue-400"
          placeholder="https://api.example.com/path"
          value={d.url}
          onChange={(e) => patchDraft(tab.id, { url: e.target.value })}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) onSend();
          }}
        />
        <button
          className="bg-blue-600 hover:bg-blue-700 text-white rounded px-5 py-1.5 text-sm font-medium disabled:opacity-50"
          disabled={tab.sending || !d.url.trim()}
          onClick={onSend}
        >
          {tab.sending ? '发送中…' : '发送'}
        </button>
        <button
          className="border rounded px-3 py-1.5 text-sm hover:bg-gray-50"
          onClick={onSave}
          title="Ctrl+S"
        >
          保存{tab.dirty ? ' •' : ''}
        </button>
        <button
          className="border rounded px-3 py-1.5 text-sm hover:bg-gray-50 text-gray-500"
          onClick={() => setShowCodegen(true)}
          title="生成代码片段"
        >
          {'</>'}
        </button>
      </div>
      {showCodegen && <CodegenDialog request={d} onClose={() => setShowCodegen(false)} />}

      {/* URL 中变量引用的解析预览 */}
      <VarPreview text={d.url ?? ''} vars={activeVars} />

      {/* 页签 */}
      <div className="flex gap-4 px-3 pt-2 border-b text-sm">
        {(
          [
            ['params', `Params${countEnabled(d.params)}`],
            ['headers', `Headers${countEnabled(d.headers)}`],
            ['body', 'Body'],
            ['auth', `Auth${d.auth?.type && d.auth.type !== 'inherit' && d.auth.type !== 'none' ? ' •' : ''}`],
            ['scripts', `脚本${d.preScript || d.testScript ? ' •' : ''}`],
            ['settings', '设置'],
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
      </div>

      {/* 页签内容 */}
      <div className="flex-1 overflow-auto p-3">
        {pane === 'params' && (
          <KVTable items={d.params ?? []} onChange={(items) => patchDraft(tab.id, { params: items })} />
        )}
        {pane === 'headers' && (
          <KVTable items={d.headers ?? []} onChange={(items) => patchDraft(tab.id, { headers: items })} />
        )}
        {pane === 'body' && (
          <div className="flex flex-col gap-2 h-full">
            <div className="flex gap-3 text-sm">
              {(
                [
                  ['none', 'none'],
                  ['raw', 'raw (JSON)'],
                  ['urlencoded', 'x-www-form-urlencoded'],
                  ['formdata', 'form-data'],
                  ['graphql', 'GraphQL'],
                  ['binary', 'binary'],
                ] as const
              ).map(([k, label]) => (
                <label key={k} className="flex items-center gap-1">
                  <input
                    type="radio"
                    checked={(d.body?.kind ?? 'none') === k}
                    onChange={() => {
                      if (k === 'raw') patchBody({ kind: 'raw', language: 'json' });
                      else if (k === 'none') patchBody({ kind: 'none' });
                      else if (k === 'graphql') patchBody({ kind: 'graphql' });
                      else if (k === 'binary') patchBody({ kind: 'binary' });
                      else patchBody({ kind: k, items: d.body?.items ?? [] });
                    }}
                  />
                  {label}
                </label>
              ))}
            </div>
            {d.body?.kind === 'raw' && (
              <div className="flex-1 border rounded overflow-hidden">
                <CodeMirror
                  height="100%"
                  style={{ height: '100%' }}
                  value={d.body.text ?? ''}
                  extensions={[json()]}
                  onChange={(text) => patchBody({ text })}
                />
              </div>
            )}
            {(d.body?.kind === 'urlencoded' || d.body?.kind === 'formdata') && (
              <KVTable
                items={(d.body.items ?? []).map(
                  (it) => ({ key: it.key, value: it.value ?? '', enabled: it.enabled }) as KV,
                )}
                onChange={(kvs) =>
                  patchBody({
                    items: kvs.map((kv) => ({
                      key: kv.key,
                      value: kv.value,
                      type: 'text',
                      enabled: kv.enabled,
                    })),
                  })
                }
              />
            )}
            {d.body?.kind === 'graphql' && (
              <div className="flex-1 flex gap-2 min-h-0">
                <div className="flex-[3] flex flex-col border rounded overflow-hidden">
                  <div className="px-2 py-1 text-xs text-gray-500 bg-gray-50 border-b">Query</div>
                  <CodeMirror
                    height="100%"
                    style={{ flex: 1, overflow: 'auto' }}
                    value={d.body.query ?? ''}
                    placeholder={'query {\n  viewer { name }\n}'}
                    onChange={(query) => patchBody({ query })}
                  />
                </div>
                <div className="flex-[2] flex flex-col border rounded overflow-hidden">
                  <div className="px-2 py-1 text-xs text-gray-500 bg-gray-50 border-b">
                    Variables (JSON)
                  </div>
                  <CodeMirror
                    height="100%"
                    style={{ flex: 1, overflow: 'auto' }}
                    value={d.body.variables ?? ''}
                    extensions={[json()]}
                    placeholder={'{}'}
                    onChange={(variables) => patchBody({ variables })}
                  />
                </div>
              </div>
            )}
            {d.body?.kind === 'binary' && (
              <div className="space-y-2 text-sm">
                <div className="flex items-center gap-2">
                  <label className="text-gray-600">文件路径</label>
                  <input
                    className="flex-1 border rounded px-2 py-1 font-mono text-xs"
                    placeholder="C:\path\to\file.bin"
                    value={d.body.path ?? ''}
                    onChange={(e) => patchBody({ path: e.target.value })}
                  />
                </div>
                <p className="text-xs text-gray-400">
                  文件以流式方式上传，不会整体读入内存；Content-Type 默认
                  application/octet-stream，可在 Headers 覆盖。
                </p>
              </div>
            )}
          </div>
        )}
        {pane === 'auth' && (
          <AuthEditor auth={d.auth} onChange={(auth) => patchDraft(tab.id, { auth })} />
        )}
        {pane === 'settings' && (
          <div className="space-y-3 max-w-md text-sm">
            <div className="flex items-center gap-2">
              <label className="text-gray-600 w-32">超时（毫秒）</label>
              <input
                type="number"
                min={0}
                className="border rounded px-2 py-1 w-32"
                value={d.settings?.timeoutMs ?? 30000}
                onChange={(e) =>
                  patchDraft(tab.id, {
                    settings: { ...d.settings, timeoutMs: Number(e.target.value) || 0 },
                  })
                }
              />
            </div>
            <label className="flex items-center gap-2 text-gray-600">
              <input
                type="checkbox"
                checked={d.settings?.followRedirects ?? true}
                onChange={(e) =>
                  patchDraft(tab.id, {
                    settings: { ...d.settings, followRedirects: e.target.checked },
                  })
                }
              />
              跟随重定向
            </label>
            {d.settings?.followRedirects && (
              <div className="flex items-center gap-2 pl-6">
                <label className="text-gray-600 w-26">最大跳数</label>
                <input
                  type="number"
                  min={1}
                  max={50}
                  className="border rounded px-2 py-1 w-20"
                  value={d.settings?.maxRedirects ?? 10}
                  onChange={(e) =>
                    patchDraft(tab.id, {
                      settings: { ...d.settings, maxRedirects: Number(e.target.value) || 10 },
                    })
                  }
                />
              </div>
            )}
            <label className="flex items-center gap-2 text-gray-600">
              <input
                type="checkbox"
                checked={d.settings?.verifyTls ?? true}
                onChange={(e) =>
                  patchDraft(tab.id, {
                    settings: { ...d.settings, verifyTls: e.target.checked },
                  })
                }
              />
              校验 SSL 证书
            </label>
            {!(d.settings?.verifyTls ?? true) && (
              <p className="text-xs text-red-500 pl-6">
                ⚠ 关闭校验后连接可被中间人截获，仅限本地调试使用
              </p>
            )}
          </div>
        )}
        {pane === 'scripts' && (
          <div className="flex flex-col gap-2 h-full">
            <div className="flex gap-3 text-sm">
              {(
                [
                  ['pre', `前置脚本${d.preScript ? ' •' : ''}`],
                  ['test', `测试脚本${d.testScript ? ' •' : ''}`],
                ] as const
              ).map(([key, label]) => (
                <label key={key} className="flex items-center gap-1">
                  <input
                    type="radio"
                    checked={scriptPhase === key}
                    onChange={() => setScriptPhase(key)}
                  />
                  {label}
                </label>
              ))}
              <span className="text-xs text-gray-400 ml-auto self-center">
                可用 pm.environment / pm.test / pm.expect / console.log
              </span>
            </div>
            <div className="flex-1 border rounded overflow-hidden">
              <CodeMirror
                height="100%"
                style={{ height: '100%' }}
                value={(scriptPhase === 'pre' ? d.preScript : d.testScript) ?? ''}
                extensions={[javascript()]}
                placeholder={
                  scriptPhase === 'pre'
                    ? `// 发送前执行，例如：\npm.environment.set('ts', Date.now());`
                    : `// 响应后执行，例如：\npm.test('status is 200', function () {\n  pm.expect(pm.response.code).to.equal(200);\n});`
                }
                onChange={(text) =>
                  patchDraft(tab.id, scriptPhase === 'pre' ? { preScript: text } : { testScript: text })
                }
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function countEnabled(items?: { enabled: boolean; key: string }[]): string {
  const n = (items ?? []).filter((i) => i.enabled && i.key).length;
  return n > 0 ? ` (${n})` : '';
}
