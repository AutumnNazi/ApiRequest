// 请求编辑器：method + URL + 发送，下方 Params/Headers/Body 页签
import { useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { json } from '@codemirror/lang-json';
import { javascript } from '@codemirror/lang-javascript';
import KVTable from './KVTable';
import type { Tab } from '../stores/tabs';
import { useTabs } from '../stores/tabs';
import type { Body } from '../ipc';

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
  onSend(): void;
  onSave(): void;
}

export default function RequestEditor({ tab, onSend, onSave }: Props) {
  const patchDraft = useTabs((s) => s.patchDraft);
  const [pane, setPane] = useState<'params' | 'headers' | 'body' | 'scripts'>('params');
  const [scriptPhase, setScriptPhase] = useState<'pre' | 'test'>('pre');
  const d = tab.draft;

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
      </div>

      {/* 页签 */}
      <div className="flex gap-4 px-3 pt-2 border-b text-sm">
        {(
          [
            ['params', `Params${countEnabled(d.params)}`],
            ['headers', `Headers${countEnabled(d.headers)}`],
            ['body', 'Body'],
            ['scripts', `脚本${d.preScript || d.testScript ? ' •' : ''}`],
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
              {['none', 'raw'].map((k) => (
                <label key={k} className="flex items-center gap-1">
                  <input
                    type="radio"
                    checked={(d.body?.kind ?? 'none') === k}
                    onChange={() =>
                      patchBody(k === 'raw' ? { kind: 'raw', language: 'json' } : { kind: 'none' })
                    }
                  />
                  {k === 'none' ? 'none' : 'raw (JSON)'}
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
