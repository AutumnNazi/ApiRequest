import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  clearCookies,
  deleteCookie,
  listCookies,
  openNativeFile,
  readNativeTextFile,
  toAppError,
  upsertCookie,
  upsertCookies,
  type Cookie,
} from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';

interface Props {
  onClose(): void;
}

interface CookieEditor {
  cookie: Cookie;
  editing: boolean;
  session: boolean;
  expiresLocal: string;
}

const inputClass =
  'w-full rounded border border-gray-200 bg-white px-2.5 py-1.5 text-xs outline-none transition-colors focus:border-blue-400 focus:ring-2 focus:ring-blue-100 disabled:bg-gray-100 disabled:text-gray-500';

export default function CookieManager({ onClose }: Props) {
  const dialog = useDialog();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState<CookieEditor | null>(null);
  const [editorError, setEditorError] = useState('');
  const query = useQuery({ queryKey: ['cookies'], queryFn: () => listCookies() });
  const cookies = query.data ?? [];
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['cookies'] });

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      if (editor) setEditor(null);
      else onClose();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [editor, onClose]);

  const save = useMutation({
    mutationFn: (cookie: Cookie) => upsertCookie(cookie),
    onSuccess: async () => {
      await invalidate();
      setEditor(null);
      setEditorError('');
    },
    onError: (cause) => setEditorError(toAppError(cause).detail),
  });
  const remove = useMutation({
    mutationFn: (cookie: { domain: string; path: string; name: string }) =>
      deleteCookie(cookie.domain, cookie.path, cookie.name),
    onSuccess: invalidate,
    onError: (cause) => void dialog.alert(
      formatMessage('删除 Cookie 失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('删除失败') },
    ),
  });
  const clearAll = useMutation({
    mutationFn: () => clearCookies(),
    onSuccess: invalidate,
    onError: (cause) => void dialog.alert(
      formatMessage('清空 Cookie 失败: {detail}', { detail: toAppError(cause).detail }),
      { title: formatMessage('清空失败') },
    ),
  });

  const openCreate = () => {
    setEditorError('');
    setEditor({ cookie: emptyCookie(), editing: false, session: true, expiresLocal: '' });
  };

  const openEdit = (cookie: Cookie) => {
    setEditorError('');
    setEditor({
      cookie: { ...cookie },
      editing: true,
      session: !cookie.expires,
      expiresLocal: cookie.expires ? toLocalDateTime(cookie.expires) : '',
    });
  };

  const submitEditor = () => {
    if (!editor || save.isPending) return;
    const domain = editor.cookie.domain?.trim() ?? '';
    const name = editor.cookie.name.trim();
    if (!domain || !name) {
      setEditorError(formatMessage('Domain 和名称不能为空'));
      return;
    }
    let expires = 0;
    if (!editor.session) {
      expires = new Date(editor.expiresLocal).getTime();
      if (!Number.isFinite(expires) || expires <= Date.now()) {
        setEditorError(formatMessage('过期时间必须晚于当前时间'));
        return;
      }
    }
    setEditorError('');
    save.mutate({
      ...editor.cookie,
      domain,
      name,
      path: editor.cookie.path?.trim() || '/',
      expires,
      maxAge: 0,
      sameSite: editor.cookie.sameSite?.toLowerCase() ?? '',
    });
  };

  const exportCookies = () => {
    const blob = new Blob([JSON.stringify(cookies, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'apirequest-cookies.json';
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  };

  const importCookies = async (content: string) => {
    try {
      const cookiesToImport = normalizeImportedCookies(JSON.parse(content) as unknown);
      await upsertCookies(cookiesToImport);
      await invalidate();
    } catch (cause) {
      void dialog.alert(
        formatMessage('导入 Cookie 失败: {detail}', { detail: toAppError(cause).detail }),
        { title: formatMessage('导入失败') },
      );
    }
  };

  const chooseImportFile = async () => {
    try {
      const path = await openNativeFile('选择 Cookie JSON 文件');
      if (path) await importCookies(await readNativeTextFile(path));
    } catch (cause) {
      void dialog.alert(
        formatMessage('导入 Cookie 失败: {detail}', { detail: toAppError(cause).detail }),
        { title: formatMessage('导入失败') },
      );
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 px-4" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="cookie-manager-title"
        className="flex h-[560px] max-h-[calc(100vh-2rem)] w-[820px] max-w-full flex-col overflow-hidden rounded-lg bg-white shadow-xl"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="flex min-h-12 shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
          <h2 id="cookie-manager-title" className="text-sm font-semibold text-gray-800">
            {formatMessage('Cookie 管理')}
          </h2>
          <span className="ml-2 text-xs tabular-nums text-gray-400">{cookies.length}</span>
          <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
            <button className="rounded bg-blue-600 px-3 py-1.5 text-xs text-white hover:bg-blue-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" onClick={openCreate}>
              {formatMessage('新建')}
            </button>
            <button className="rounded border px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" onClick={() => void chooseImportFile()}>
              {formatMessage('导入')}
            </button>
            <button className="rounded border px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" onClick={exportCookies} disabled={cookies.length === 0}>
              {formatMessage('导出')}
            </button>
            <button
              className="rounded border border-red-200 px-3 py-1.5 text-xs text-red-600 hover:bg-red-50 disabled:opacity-40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-400"
              disabled={cookies.length === 0 || clearAll.isPending}
              onClick={() => void dialog.confirm(formatMessage('清空全部 Cookie？')).then((ok) => ok && clearAll.mutate())}
            >
              {formatMessage('全部清空')}
            </button>
            <button className="flex h-8 w-8 items-center justify-center text-lg text-gray-400 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" onClick={onClose} aria-label={formatMessage('关闭')} title={formatMessage('关闭')}>
              ×
            </button>
          </div>
        </header>

        {editor && (
          <CookieEditorBand
            editor={editor}
            error={editorError}
            pending={save.isPending}
            onChange={setEditor}
            onCancel={() => setEditor(null)}
            onSubmit={submitEditor}
          />
        )}

        <div className="min-h-0 flex-1 overflow-auto">
          {query.isPending ? (
            <div className="flex h-full items-center justify-center text-sm text-gray-400" role="status">
              {formatMessage('加载中…')}
            </div>
          ) : query.isError ? (
            <div className="flex h-full items-center justify-center px-6 text-sm text-red-600" role="alert">
              <Verbatim value={toAppError(query.error).detail} />
            </div>
          ) : cookies.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-gray-400">
              {formatMessage('暂无 Cookie；响应中的 Set-Cookie 会自动存入')}
            </div>
          ) : (
            <CookieTable cookies={cookies} onEdit={openEdit} onDelete={(cookie) => remove.mutate(cookie)} />
          )}
        </div>
      </div>
    </div>
  );
}

function CookieEditorBand({
  editor,
  error,
  pending,
  onChange,
  onCancel,
  onSubmit,
}: {
  editor: CookieEditor;
  error: string;
  pending: boolean;
  onChange(editor: CookieEditor): void;
  onCancel(): void;
  onSubmit(): void;
}) {
  const update = (patch: Partial<Cookie>) => onChange({ ...editor, cookie: { ...editor.cookie, ...patch } });
  return (
    <form
      className="shrink-0 border-b bg-gray-50 px-4 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-[1.4fr_0.8fr_1fr_1.4fr]">
        <label className="text-[11px] font-medium text-gray-500">
          Domain
          <input autoFocus={!editor.editing} className={`${inputClass} mt-1 font-mono`} value={editor.cookie.domain ?? ''} disabled={editor.editing} onChange={(event) => update({ domain: event.target.value })} />
        </label>
        <label className="text-[11px] font-medium text-gray-500">
          {formatMessage('路径')}
          <input className={`${inputClass} mt-1 font-mono`} value={editor.cookie.path ?? '/'} disabled={editor.editing} onChange={(event) => update({ path: event.target.value })} />
        </label>
        <label className="text-[11px] font-medium text-gray-500">
          {formatMessage('名称')}
          <input className={`${inputClass} mt-1 font-mono`} value={editor.cookie.name} disabled={editor.editing} onChange={(event) => update({ name: event.target.value })} />
        </label>
        <label className="text-[11px] font-medium text-gray-500">
          {formatMessage('值')}
          <input autoFocus={editor.editing} className={`${inputClass} mt-1 font-mono`} value={editor.cookie.value} onChange={(event) => update({ value: event.target.value })} />
        </label>
      </div>
      <div className="mt-3 flex min-h-8 flex-wrap items-center gap-x-4 gap-y-2">
        <label className="flex items-center gap-1.5 text-xs text-gray-600">
          <input type="checkbox" checked={editor.cookie.hostOnly} onChange={(event) => update({ hostOnly: event.target.checked })} />
          Host-only
        </label>
        <label className="flex items-center gap-1.5 text-xs text-gray-600">
          <input type="checkbox" checked={editor.cookie.httpOnly} onChange={(event) => update({ httpOnly: event.target.checked })} />
          HttpOnly
        </label>
        <label className="flex items-center gap-1.5 text-xs text-gray-600">
          <input type="checkbox" checked={editor.cookie.secure} onChange={(event) => update({ secure: event.target.checked })} />
          Secure
        </label>
        <label className="flex items-center gap-1.5 text-xs text-gray-600">
          SameSite
          <select className="rounded border border-gray-200 bg-white px-2 py-1 text-xs focus:border-blue-400 focus:outline-none" value={editor.cookie.sameSite ?? ''} onChange={(event) => update({ sameSite: event.target.value })}>
            <option value="">Default</option>
            <option value="lax">Lax</option>
            <option value="strict">Strict</option>
            <option value="none">None</option>
          </select>
        </label>
        <label className="flex items-center gap-1.5 text-xs text-gray-600">
          <input type="checkbox" checked={editor.session} onChange={(event) => onChange({ ...editor, session: event.target.checked })} />
          {formatMessage('会话 Cookie')}
        </label>
        {!editor.session && (
          <input aria-label={formatMessage('过期时间')} type="datetime-local" className="rounded border border-gray-200 bg-white px-2 py-1 text-xs focus:border-blue-400 focus:outline-none" value={editor.expiresLocal} onChange={(event) => onChange({ ...editor, expiresLocal: event.target.value })} />
        )}
        <div className="ml-auto flex items-center gap-2">
          {error && <span className="max-w-64 truncate text-xs text-red-600" role="alert" title={error}><Verbatim value={error} /></span>}
          <button type="button" className="rounded border px-3 py-1.5 text-xs text-gray-600 hover:bg-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" onClick={onCancel}>{formatMessage('取消')}</button>
          <button type="submit" className="rounded bg-blue-600 px-3 py-1.5 text-xs text-white hover:bg-blue-700 disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" disabled={pending}>{pending ? formatMessage('保存中…') : formatMessage('保存')}</button>
        </div>
      </div>
    </form>
  );
}

function CookieTable({ cookies, onEdit, onDelete }: {
  cookies: Cookie[];
  onEdit(cookie: Cookie): void;
  onDelete(cookie: { domain: string; path: string; name: string }): void;
}) {
  const groups = useMemo(() => {
    const grouped = new Map<string, Cookie[]>();
    for (const cookie of cookies) {
      const domain = cookie.domain || '(无域)';
      grouped.set(domain, [...(grouped.get(domain) ?? []), cookie]);
    }
    return [...grouped.entries()];
  }, [cookies]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggle = (domain: string) => setCollapsed((current) => {
    const next = new Set(current);
    if (next.has(domain)) next.delete(domain);
    else next.add(domain);
    return next;
  });

  return (
    <div className="text-xs">
      {groups.map(([domain, items]) => (
        <section key={domain}>
          <button className="sticky top-0 z-10 flex w-full items-center gap-1 border-b border-gray-200 bg-gray-100 px-3 py-1.5 text-left hover:bg-gray-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500" onClick={() => toggle(domain)} aria-expanded={!collapsed.has(domain)}>
            <span className="w-3 text-gray-500">{collapsed.has(domain) ? '›' : '⌄'}</span>
            <span className="font-mono font-medium text-gray-700"><Verbatim value={domain} /></span>
            <span className="text-gray-400">({items.length})</span>
          </button>
          {!collapsed.has(domain) && (
            <table className="w-full min-w-[680px] table-fixed">
              <thead className="sr-only"><tr><th>{formatMessage('路径')}</th><th>{formatMessage('名称')}</th><th>{formatMessage('值')}</th><th>{formatMessage('属性')}</th><th>{formatMessage('过期时间')}</th><th>{formatMessage('操作')}</th></tr></thead>
              <tbody>
                {items.map((cookie) => {
                  const expires = cookie.expires ? new Date(cookie.expires) : null;
                  const expired = Boolean(expires && expires.getTime() < Date.now());
                  const flags = [cookie.hostOnly && 'Host', cookie.httpOnly && 'HttpOnly', cookie.secure && 'Secure', cookie.sameSite].filter(Boolean).join(' · ');
                  return (
                    <tr key={cookieKey(cookie)} className="border-b border-gray-100 hover:bg-gray-50">
                      <td className="w-[12%] truncate p-2 font-mono text-gray-500"><Verbatim value={cookie.path || '/'} /></td>
                      <td className="w-[18%] truncate p-2 font-mono font-medium text-gray-800"><Verbatim value={cookie.name} /></td>
                      <td className="w-[24%] truncate p-2 font-mono text-gray-700" title={cookie.value} data-i18n-verbatim><Verbatim value={cookie.value} /></td>
                      <td className="w-[18%] truncate p-2 text-[11px] text-gray-400"><Verbatim value={flags || '—'} /></td>
                      <td className={`w-[20%] truncate p-2 ${expired ? 'text-red-600' : 'text-gray-400'}`}><Verbatim value={formatExpiry(expires, expired)} /></td>
                      <td className="w-[8%] p-1.5 text-right">
                        <button className="px-1.5 py-1 text-gray-500 hover:text-blue-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500" onClick={() => onEdit(cookie)} aria-label={formatMessage('编辑 Cookie')} title={formatMessage('编辑')}>{formatMessage('编辑')}</button>
                        <button className="px-1.5 py-1 text-base leading-none text-gray-400 hover:text-red-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-400" onClick={() => onDelete({ domain: cookie.domain ?? '', path: cookie.path || '/', name: cookie.name })} aria-label={formatMessage('删除 Cookie')} title={formatMessage('删除')}>×</button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </section>
      ))}
    </div>
  );
}

function emptyCookie(): Cookie {
  return {
    name: '', value: '', domain: '', path: '/', expires: 0, maxAge: 0,
    httpOnly: false, secure: false, sameSite: '', hostOnly: true,
  };
}

function toLocalDateTime(timestamp: number): string {
  const date = new Date(timestamp - new Date(timestamp).getTimezoneOffset() * 60_000);
  return date.toISOString().slice(0, 16);
}

function formatExpiry(expires: Date | null, expired: boolean): string {
  if (!expires) return formatMessage('会话');
  const date = expires.toLocaleString();
  if (expired) return formatMessage('{date}（已过期）', { date });
  const remaining = expires.getTime() - Date.now();
  if (remaining < 86_400_000) return formatMessage('{date}（剩 {count} 小时）', { date, count: Math.max(0, Math.round(remaining / 3_600_000)) });
  return formatMessage('{date}（剩 {count} 天）', { date, count: Math.round(remaining / 86_400_000) });
}

function cookieKey(cookie: Cookie): string {
  return [cookie.domain, cookie.path || '/', cookie.name].join('|');
}

export function normalizeImportedCookies(parsed: unknown): Cookie[] {
  if (!Array.isArray(parsed)) throw new Error(formatMessage('文件根节点必须是 Cookie 数组'));
  return parsed.map((value, index) => {
    const label = formatMessage('第 {index} 个 Cookie', { index: index + 1 });
    if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(formatMessage('{label} 必须是对象', { label }));
    const raw = value as Record<string, unknown>;
    if (typeof raw.name !== 'string' || !raw.name.trim()) throw new Error(formatMessage('{label} 的 name 必须是非空字符串', { label }));
    if (typeof raw.domain !== 'string' || !raw.domain.trim()) throw new Error(formatMessage('{label} 的 domain 必须是非空字符串', { label }));
    if (raw.value !== undefined && typeof raw.value !== 'string') throw new Error(formatMessage('{label} 的 value 必须是字符串', { label }));
    if (raw.path !== undefined && typeof raw.path !== 'string') throw new Error(formatMessage('{label} 的 path 必须是字符串', { label }));
    if (raw.expires !== undefined && (typeof raw.expires !== 'number' || !Number.isFinite(raw.expires))) throw new Error(formatMessage('{label} 的 expires 必须是有限数字', { label }));
    for (const field of ['httpOnly', 'secure', 'hostOnly'] as const) {
      if (raw[field] !== undefined && typeof raw[field] !== 'boolean') throw new Error(formatMessage('{label} 的 {field} 必须是布尔值', { label, field }));
    }
    const sameSite = typeof raw.sameSite === 'string' ? raw.sameSite.toLowerCase() : '';
    if (!['', 'lax', 'strict', 'none'].includes(sameSite)) throw new Error(formatMessage('{label} 的 SameSite 无效', { label }));
    const domain = raw.domain.trim();
    return {
      name: raw.name.trim(), value: (raw.value as string | undefined) ?? '', domain,
      path: (raw.path as string | undefined) || '/', expires: (raw.expires as number | undefined) ?? 0,
      maxAge: 0, httpOnly: (raw.httpOnly as boolean | undefined) ?? false,
      secure: (raw.secure as boolean | undefined) ?? false, sameSite,
      hostOnly: (raw.hostOnly as boolean | undefined) ?? !domain.startsWith('.'),
    };
  });
}
