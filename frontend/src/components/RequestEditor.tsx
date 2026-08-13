// 请求编辑器：method + URL + 发送，下方 Params/Headers/Body 页签
import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import CodeMirror from '@uiw/react-codemirror';
import { json } from '@codemirror/lang-json';
import { javascript } from '@codemirror/lang-javascript';
import KVTable, { COMMON_HEADERS } from './KVTable';
import AuthEditor from './AuthEditor';
import CodegenDialog from './CodegenDialog';
import VarPreview, { useActiveVariables, VarPreviewMulti } from './VarPreview';
import type { Tab } from '../stores/tabs';
import { useTabs } from '../stores/tabs';
import { generateCode, openNativeFile, toAppError, listHistory, type Body, type FormItem, type KV } from '../ipc';
import { useStableRowIds } from '../hooks/useStableRowIds';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];

// CodeMirror 扩展只需创建一次，避免每次渲染重建导致编辑器反复重配置
const jsonExtensions = [json()];
const jsExtensions = [javascript()];

// 生成 cURL 命令并写入剪贴板
function copyAsCurl(d: Tab['draft']): Promise<void> {
  return generateCode('curl', d).then((code) => navigator.clipboard.writeText(code));
}

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
  onCancel(): void;
  onSave(): void;
}

export default function RequestEditor({ tab, workspaceId, onSend, onCancel, onSave }: Props) {
  const dialog = useDialog();
  const patchDraft = useTabs((s) => s.patchDraft);
  const [pane, setPane] = useState<'params' | 'headers' | 'body' | 'auth' | 'scripts' | 'settings'>('params');
  const [scriptPhase, setScriptPhase] = useState<'pre' | 'test'>('pre');
  const [showCodegen, setShowCodegen] = useState(false);
  const [urlSuggestOpen, setUrlSuggestOpen] = useState(false);
  const [urlSuggestDebounced, setUrlSuggestDebounced] = useState('');
  const urlInputRef = useRef<HTMLInputElement>(null);
  const d = tab.draft;
  const activeVars = useActiveVariables(workspaceId);

  // URL 输入建议：基于历史记录联想最近使用的地址（输入停顿后查询）
  useEffect(() => {
    const timer = setTimeout(() => setUrlSuggestDebounced(d.url), 400);
    return () => clearTimeout(timer);
  }, [d.url]);

  const { data: urlSuggestions = [] } = useQuery({
    queryKey: ['url-suggestions', workspaceId, urlSuggestDebounced],
    queryFn: async () => {
      const page = await listHistory(workspaceId, { search: urlSuggestDebounced.trim(), limit: 8 });
      return page.items
        .map((it) => it.url)
        .filter((u) => u && Boolean(u))
        .filter((u, i, arr) => arr.indexOf(u) === i)
        .slice(0, 8);
    },
    enabled: urlSuggestOpen && urlSuggestDebounced.trim().length > 0,
  });

  const applyUrlSuggestion = (url: string) => {
    patchDraft(tab.id, { url });
    setUrlSuggestOpen(false);
    urlInputRef.current?.focus();
  };

  const patchBody = (patch: Partial<Body>) =>
    patchDraft(tab.id, { body: { ...d.body, ...patch } as Body });

  return (
    <div className="flex flex-col h-full">
      {/* method + url + 按钮行 */}
      <div className="flex gap-2 p-3 border-b">
        <input
          list="http-methods"
          type="text"
          className={`shrink-0 border rounded px-2 py-1.5 font-semibold text-sm ${methodColor[d.method] ?? ''}`}
          value={d.method}
          autoComplete="off"
          spellCheck={false}
          onChange={(e) => patchDraft(tab.id, { method: e.target.value.toUpperCase() })}
        />
        <datalist id="http-methods">
          {METHODS.map((m) => (
            <option key={m} value={m} />
          ))}
        </datalist>
        <div className="relative flex-1">
          <input
            ref={urlInputRef}
            className="w-full border rounded px-3 py-1.5 pr-24 text-sm font-mono outline-none focus:border-blue-400"
            placeholder="https://api.example.com/path"
            value={d.url}
            onFocus={() => setUrlSuggestOpen(true)}
            onBlur={() => setTimeout(() => setUrlSuggestOpen(false), 150)}
            onChange={(e) => {
              patchDraft(tab.id, { url: e.target.value });
              setUrlSuggestOpen(true);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                e.stopPropagation();
                onSend();
              }
              if (e.key === 'Escape') setUrlSuggestOpen(false);
            }}
          />
          {urlSuggestOpen && urlSuggestions.length > 0 && (
            <div className="absolute top-full left-0 z-30 mt-0.5 w-full overflow-auto rounded border bg-white py-1 text-xs shadow-lg max-h-56">
              {urlSuggestions.map((u) => (
                <button
                  key={u}
                  type="button"
                  className="block w-full truncate px-2 py-1 text-left text-gray-700 font-mono hover:bg-blue-50"
                  onMouseDown={(e) => {
                    e.preventDefault();
                    applyUrlSuggestion(u);
                  }}
                >
                  <Verbatim value={u} />
                </button>
              ))}
            </div>
          )}
          <div className="absolute right-1 top-1/2 flex -translate-y-1/2 items-center gap-0.5">
            <button
              type="button"
              className="rounded px-1.5 py-0.5 text-xs text-gray-400 hover:bg-gray-100 hover:text-gray-700"
              title={formatMessage('切换 http/https')}
              onClick={() => {
                const u = d.url.trim();
                if (u.startsWith('https://')) patchDraft(tab.id, { url: 'http://' + u.slice(8) });
                else if (u.startsWith('http://')) patchDraft(tab.id, { url: 'https://' + u.slice(7) });
                else patchDraft(tab.id, { url: 'https://' + u });
              }}
            >
              ⇄
            </button>
            <button
              type="button"
              className="rounded px-1.5 py-0.5 text-xs text-gray-400 hover:bg-gray-100 hover:text-gray-700"
              title={formatMessage('复制 URL')}
              onClick={() => void navigator.clipboard.writeText(d.url).catch(() => {})}
            >
              {formatMessage('复制')}
            </button>
            <button
              type="button"
              className="rounded px-1.5 py-0.5 text-xs text-gray-400 hover:bg-gray-100 hover:text-gray-700"
              title={formatMessage('复制为 cURL')}
              onClick={() => void copyAsCurl(d).catch(() => {})}
            >
              cURL
            </button>
          </div>
        </div>
        <button
          className="bg-blue-600 hover:bg-blue-700 text-white rounded px-5 py-1.5 text-sm font-medium disabled:opacity-50"
          disabled={tab.sending || !d.url.trim()}
          onClick={onSend}
        >
          {tab.sending ? formatMessage('发送中…') : formatMessage('发送')}
        </button>
        {tab.sending && (
          <button
            className="border border-red-200 text-red-600 rounded px-3 py-1.5 text-sm hover:bg-red-50"
            onClick={onCancel}
            title={formatMessage('取消当前请求')}
          >
            {formatMessage('取消')}
          </button>
        )}
        <button
          className="border rounded px-3 py-1.5 text-sm hover:bg-gray-50"
          onClick={onSave}
          title="Ctrl+S"
        >
          {formatMessage('保存')}{tab.dirty ? ' •' : ''}
        </button>
        <button
          className="border rounded px-3 py-1.5 text-sm hover:bg-gray-50 text-gray-500"
          onClick={() => setShowCodegen(true)}
          title={formatMessage('生成代码片段')}
        >
          {'</>'}
        </button>
      </div>
      {showCodegen && <CodegenDialog request={d} onClose={() => setShowCodegen(false)} />}

      {/* URL + Body 中变量引用的解析预览（合并去重，覆盖 URL / raw / GraphQL Query / Variables） */}
      <VarPreviewMulti
        texts={[
          d.url ?? '',
          d.body?.kind === 'raw' ? d.body.text ?? '' : '',
          d.body?.kind === 'graphql' ? `${d.body.query ?? ''}\n${d.body.variables ?? ''}` : '',
          d.body?.kind === 'urlencoded'
            ? (d.body.items ?? []).map((i) => `${i.key}=${i.value ?? ''}`).join('&')
            : '',
        ]}
        vars={activeVars}
      />

      {/* 页签 */}
      <div className="flex gap-1 px-3 pt-2 border-b text-sm">
        {(
          [
            ['params', `Params${countEnabled(d.params)}`],
            ['headers', `Headers${countEnabled(d.headers)}`],
            ['body', 'Body'],
            ['auth', `Auth${d.auth?.type && d.auth.type !== 'inherit' && d.auth.type !== 'none' ? ' •' : ''}`],
            ['scripts', formatMessage('脚本{marker}', { marker: d.preScript || d.testScript ? ' •' : '' })],
            ['settings', formatMessage('设置')],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            className={`px-3 py-1.5 mb-1 rounded ${
              pane === key
                ? 'bg-gray-100 text-gray-900 font-medium'
                : 'text-gray-500 hover:text-gray-800'
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
          <KVTable
            key={`${tab.id}:params`}
            items={d.params ?? []}
            onChange={(items) => patchDraft(tab.id, { params: items })}
          />
        )}
        {pane === 'headers' && (
          <KVTable
            key={`${tab.id}:headers`}
            items={d.headers ?? []}
            onChange={(items) => patchDraft(tab.id, { headers: items })}
            keySuggestions={COMMON_HEADERS}
          />
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
                      // 切换 kind 时显式丢弃不相关字段，避免 stale IR（例 kind=binary 但 items 残留）。
                      // 注意：patchBody 用 Partial<Body>，所以只覆盖写了的字段；保留复用上一次
                      // 的 items（仅在 formdata / urlencoded 间切时）。
                      switch (k) {
                        case 'raw':       patchBody({ kind: 'raw', language: 'json', text: '' }); break;
                        case 'none':      patchBody({ kind: 'none', text: '', items: [], path: '', query: '', variables: '' }); break;
                        case 'graphql':   patchBody({ kind: 'graphql', text: '', items: [], path: '', query: '', variables: '' }); break;
                        case 'binary':    patchBody({ kind: 'binary', text: '', items: [], path: '', query: '', variables: '' }); break;
                        case 'formdata':  patchBody({ kind: 'formdata', text: '', items: d.body?.items ?? [], path: '', query: '', variables: '' }); break;
                        case 'urlencoded':patchBody({ kind: 'urlencoded', text: '', items: d.body?.items ?? [], path: '', query: '', variables: '' }); break;
                      }
                    }}
                  />
                  {label}
                </label>
              ))}
            </div>
            {d.body?.kind === 'raw' && (
              <div className="flex-1 flex flex-col border rounded overflow-hidden">
                <div className="flex items-center justify-between px-2 py-1 bg-gray-50 border-b">
                  <span className="text-xs text-gray-400">JSON</span>
                  <div className="flex items-center gap-2">
                    <button
                      className="text-xs text-blue-600 hover:text-blue-800"
                      onClick={() => {
                        try {
                          const parsed = JSON.parse(d.body?.text ?? '');
                          patchBody({ text: JSON.stringify(parsed, null, 2) });
                        } catch (e) {
                          void dialog.alert(
                            formatMessage('JSON 格式化失败: {detail}', { detail: (e as Error).message }),
                            { title: formatMessage('格式化失败') },
                          );
                        }
                      }}
                    >
                      {formatMessage('格式化')}
                    </button>
                    <button
                      className="text-xs text-blue-600 hover:text-blue-800"
                      onClick={() => {
                        try {
                          const parsed = JSON.parse(d.body?.text ?? '');
                          patchBody({ text: JSON.stringify(parsed) });
                        } catch (e) {
                          void dialog.alert(
                            formatMessage('JSON 压缩失败: {detail}', { detail: (e as Error).message }),
                            { title: formatMessage('压缩失败') },
                          );
                        }
                      }}
                    >
                      {formatMessage('压缩')}
                    </button>
                  </div>
                </div>
                <CodeMirror
                  height="100%"
                  style={{ flex: 1, overflow: 'auto' }}
                  value={d.body.text ?? ''}
                  extensions={jsonExtensions}
                  onChange={(text) => patchBody({ text })}
                />
              </div>
            )}
            {d.body?.kind === 'urlencoded' && (
              <KVTable
                key={`${tab.id}:urlencoded`}
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
            {d.body?.kind === 'formdata' && (
              <FormDataTable
                key={`${tab.id}:formdata`}
                items={d.body.items ?? []}
                onChange={(items) => patchBody({ items })}
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
                    extensions={jsonExtensions}
                    placeholder={'{}'}
                    onChange={(variables) => patchBody({ variables })}
                  />
                  {d.body.variables?.trim() && (() => {
                    try { JSON.parse(d.body.variables ?? ''); return null; } catch (e) {
                      return (
                        <div className="px-2 py-1 text-xs text-red-600 bg-red-50 border-t">
                          {formatMessage('JSON 语法错误：')}<Verbatim value={(e as Error).message} />
                        </div>
                      );
                    }
                  })()}
                </div>
              </div>
            )}
            {d.body?.kind === 'binary' && (
              <div className="space-y-2 text-sm">
                <div className="flex items-center gap-2">
                  <label className="text-gray-600">{formatMessage('文件路径')}</label>
                  <input
                    className="min-w-0 flex-1 border rounded px-2 py-1 font-mono text-xs"
                    placeholder="C:\path\to\file.bin"
                    value={d.body.path ?? ''}
                    onChange={(e) => patchBody({ path: e.target.value })}
                  />
                  <button
                    className="border rounded px-2 py-1 text-xs hover:bg-gray-50"
                    onClick={async () => {
                      try {
                        const path = await openNativeFile('选择二进制请求文件');
                        if (path) patchBody({ path });
                      } catch (cause) {
                        void dialog.alert(
                          formatMessage('选择文件失败: {detail}', { detail: toAppError(cause).detail }),
                          { title: '选择文件失败' },
                        );
                      }
                    }}
                  >
                    {formatMessage('浏览…')}
                  </button>
                </div>
                <p className="text-xs text-gray-400">
                  {formatMessage('文件以流式方式上传，不会整体读入内存；Content-Type 默认 application/octet-stream，可在 Headers 覆盖。')}
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
              <label className="text-gray-600 w-32">{formatMessage('超时（毫秒）')}</label>
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
              {formatMessage('跟随重定向')}
            </label>
            {d.settings?.followRedirects && (
              <div className="flex items-center gap-2 pl-6">
                <label className="text-gray-600 w-26">{formatMessage('最大跳数')}</label>
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
              {formatMessage('校验 SSL 证书')}
            </label>
            {!(d.settings?.verifyTls ?? true) && (
              <p className="text-xs text-red-500 pl-6">
                {formatMessage('⚠ 关闭校验后连接可被中间人截获，仅限本地调试使用')}
              </p>
            )}
          </div>
        )}
        {pane === 'scripts' && (
          <div className="flex flex-col gap-2 h-full">
            <div className="flex gap-3 text-sm">
              {(
                [
                  ['pre', formatMessage('前置脚本{marker}', { marker: d.preScript ? ' •' : '' })],
                  ['test', formatMessage('测试脚本{marker}', { marker: d.testScript ? ' •' : '' })],
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
                {formatMessage('可用 pm.environment / pm.test / pm.expect / console.log')}
              </span>
            </div>
            <div className="flex-1 border rounded overflow-hidden">
              <CodeMirror
                height="100%"
                style={{ height: '100%' }}
                value={(scriptPhase === 'pre' ? d.preScript : d.testScript) ?? ''}
                extensions={jsExtensions}
                placeholder={
                  scriptPhase === 'pre'
                    ? formatMessage(`// 发送前执行，例如：\npm.environment.set('ts', Date.now());`)
                    : formatMessage(`// 响应后执行，例如：\npm.test('status is 200', function () {\n  pm.expect(pm.response.code).to.equal(200);\n});`)
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

// form-data 表格：支持 text / file 两种条目（file 型走 Path，后端 multipart 读取）
function FormDataTable({
  items,
  onChange,
}: {
  items: FormItem[];
  onChange(items: FormItem[]): void;
}) {
  const dialog = useDialog();
  const rows: FormItem[] = [
    ...items,
    { key: '', type: 'text', value: '', path: '', enabled: true },
  ];
  const { rowIds, promoteGhostRow, removeRow } = useStableRowIds(rows.length);

  const update = (idx: number, patch: Partial<FormItem>) => {
    const next = rows.map((r, i) => (i === idx ? { ...r, ...patch } : r));
    const nextItems = next.filter(
      (r, i) => !(i === next.length - 1 && !r.key && !r.value && !r.path),
    );
    if (nextItems.length > items.length) promoteGhostRow();
    onChange(nextItems);
  };

  const remove = (idx: number) => {
    removeRow(idx);
    onChange(items.filter((_, i) => i !== idx));
  };

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="text-left text-gray-500 border-b">
          <th className="w-8 p-1"></th>
          <th className="p-1 font-normal w-16">{formatMessage('类型')}</th>
          <th className="p-1 font-normal">Key</th>
          <th className="p-1 font-normal">{formatMessage('Value / 文件路径')}</th>
          <th className="w-8 p-1"></th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => {
          const isGhost = i === rows.length - 1;
          const isFile = r.type === 'file';
          return (
            <tr key={rowIds[i]} className="border-b border-gray-100">
              <td className="p-1 text-center">
                {!isGhost && (
                  <input
                    type="checkbox"
                    checked={r.enabled}
                    onChange={(e) => update(i, { enabled: e.target.checked })}
                  />
                )}
              </td>
              <td className="p-1">
                {!isGhost ? (
                  <select
                    className="border rounded px-1 py-0.5 text-xs"
                    value={isFile ? 'file' : 'text'}
                    onChange={(e) =>
                      update(i, {
                        type: e.target.value,
                        // 切类型时清掉对方字段，避免 stale
                        ...(e.target.value === 'file'
                          ? { value: '', path: r.path ?? '' }
                          : { path: '', value: r.value ?? '' }),
                      })
                    }
                  >
                    <option value="text">text</option>
                    <option value="file">file</option>
                  </select>
                ) : (
                  <span className="text-xs text-gray-400">text</span>
                )}
              </td>
              <td className="p-1">
                <input
                  className="w-full px-1 py-0.5 outline-none focus:bg-blue-50"
                  placeholder="Key"
                  value={r.key}
                  onChange={(e) => update(i, { key: e.target.value })}
                />
              </td>
              <td className="p-1">
                <div className="flex gap-1">
                  <input
                    className="min-w-0 flex-1 px-1 py-0.5 outline-none focus:bg-blue-50 font-mono text-xs"
                    placeholder={isFile ? 'C:\\path\\to\\file' : 'Value'}
                    value={isFile ? (r.path ?? '') : (r.value ?? '')}
                    onChange={(e) =>
                      update(i, isFile ? { path: e.target.value, type: 'file' } : { value: e.target.value, type: 'text' })
                    }
                  />
                  {isFile && !isGhost && (
                    <button
                      className="shrink-0 border rounded px-1.5 text-xs hover:bg-gray-50"
                      onClick={async () => {
                        try {
                          const path = await openNativeFile('选择 multipart 文件');
                          if (path) update(i, { path, type: 'file', value: '' });
                        } catch (cause) {
                          void dialog.alert(
                            formatMessage('选择文件失败: {detail}', { detail: toAppError(cause).detail }),
                            { title: '选择文件失败' },
                          );
                        }
                      }}
                    >
                      {formatMessage('浏览…')}
                    </button>
                  )}
                </div>
              </td>
              <td className="p-1 text-center">
                {!isGhost && (
                  <button
                    className="text-gray-400 hover:text-red-500"
                    onClick={() => remove(i)}
                    title={formatMessage('删除')}
                  >
                    ×
                  </button>
                )}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
