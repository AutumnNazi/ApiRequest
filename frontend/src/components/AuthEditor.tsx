// Auth 编辑：类型选择 + 按类型渲染参数表单；OAuth2 含"获取 Token"流程
import { useState } from 'react';
import { getOAuth2Token, clearOAuth2Token, toAppError } from '../ipc';
import type { Auth } from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';
import { AUTH_FIELDS, AUTH_TYPES } from '../authPolicy';

interface Props {
  auth: Auth;
  onChange(auth: Auth): void;
}

export default function AuthEditor({ auth, onChange }: Props) {
  const type = auth?.type ?? 'none';
  const params = auth?.params ?? {};
  const fields = AUTH_FIELDS[type] ?? [];
  const [tokenState, setTokenState] = useState<'idle' | 'fetching' | 'ok' | 'error'>('idle');
  const [tokenMsg, setTokenMsg] = useState('');

  const fetchToken = async () => {
    setTokenState('fetching');
    setTokenMsg(
      params.grantType === 'authorization_code' || !params.grantType
        ? formatMessage('已拉起浏览器，请完成授权（本地回调）…')
        : '',
    );
    try {
      const tok = await getOAuth2Token(params);
      onChange({ type, params: { ...params, accessToken: tok.accessToken } } as Auth);
      setTokenState('ok');
      setTokenMsg(
        tok.expiresAt
          ? formatMessage('Token 已获取，{time} 过期', {
              time: new Date(tok.expiresAt).toLocaleTimeString(),
            })
          : formatMessage('Token 已获取'),
      );
    } catch (e) {
      setTokenState('error');
      setTokenMsg(toAppError(e).detail);
    }
  };

  const clearToken = async () => {
    await clearOAuth2Token(params);
    const { accessToken: _drop, ...rest } = params;
    onChange({ type, params: rest } as Auth);
    setTokenState('idle');
    setTokenMsg('');
  };

  return (
    <div className="space-y-3 max-w-lg">
      <div className="flex items-center gap-2">
        <label className="text-sm text-gray-600 w-20">{formatMessage('认证类型')}</label>
        <select
          className="border rounded px-2 py-1 text-sm flex-1"
          value={type}
          onChange={(e) => {
            // 切类型时保留已有 params 字段，只替换 type（用户可能从一种认证切回，不应丢值）
            onChange({ ...auth, type: e.target.value });
          }}
        >
          {AUTH_TYPES.map(([value, label]) => (
            <option key={value} value={value}>
              {formatMessage(label)}
            </option>
          ))}
        </select>
      </div>
      {fields.map(([key, label, secret]) => {
        const t = formatMessage(label);
        const openIdx = t.search(/[（(]/);
        const labelText = openIdx >= 0 ? t.slice(0, openIdx) : t;
        const placeholderText = openIdx >= 0 ? t.slice(openIdx + 1, -1) : '';
        return (
          <div key={key} className="flex items-center gap-2">
            <label className="text-sm text-gray-600 w-20 shrink-0">{labelText}</label>
            <input
              className="border rounded px-2 py-1 text-sm flex-1 font-mono"
              type={secret ? 'password' : 'text'}
              placeholder={placeholderText}
              value={params[key] ?? ''}
              onChange={(e) =>
                onChange({ type, params: { ...params, [key]: e.target.value } } as Auth)
              }
            />
          </div>
        );
      })}
      {type === 'oauth2' && (
        <div className="space-y-2 pt-1">
          <div className="flex items-center gap-2">
            <button
              className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
              disabled={tokenState === 'fetching' || !params.tokenUrl}
              onClick={fetchToken}
            >
              {tokenState === 'fetching' ? formatMessage('获取中…') : formatMessage('获取 Token')}
            </button>
            {params.accessToken && (
              <>
                <span className="text-xs text-green-600 font-mono">
                  <Verbatim value={params.accessToken.slice(0, 24)} />…
                </span>
                <button className="text-xs text-red-500 hover:underline" onClick={clearToken}>
                  {formatMessage('清除')}
                </button>
              </>
            )}
          </div>
          {tokenMsg && (
            <p className={`text-xs ${tokenState === 'error' ? 'text-red-600' : 'text-gray-500'}`}>
              <Verbatim value={tokenMsg} />
            </p>
          )}
        </div>
      )}
      {fields.length > 0 && (
        <p className="text-xs text-gray-400">{formatMessage('字段值可用 {变量名} 引用环境/全局变量，发送时自动替换。')}</p>
      )}
    </div>
  );
}
