// Auth 编辑：类型选择 + 按类型渲染参数表单；OAuth2 含"获取 Token"流程
import { useState } from 'react';
import { getOAuth2Token, clearOAuth2Token, toAppError } from '../ipc';
import type { Auth } from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';

const AUTH_TYPES: [string, string][] = [
  ['inherit', '继承父级'],
  ['none', 'No Auth'],
  ['basic', 'Basic Auth'],
  ['bearer', 'Bearer Token'],
  ['apikey', 'API Key'],
  ['digest', 'Digest'],
  ['oauth1', 'OAuth 1.0'],
  ['oauth2', 'OAuth 2.0'],
  ['awsv4', 'AWS Signature V4'],
];

// 每种类型的参数字段定义：[param key, label, secret?]
const FIELDS: Record<string, [string, string, boolean?][]> = {
  basic: [
    ['username', '用户名'],
    ['password', '密码', true],
  ],
  bearer: [['token', 'Token', true]],
  apikey: [
    ['key', 'Key 名称'],
    ['value', '值', true],
    ['in', '位置（header/query）'],
  ],
  digest: [
    ['username', '用户名'],
    ['password', '密码', true],
  ],
  oauth1: [
    ['consumerKey', 'Consumer Key'],
    ['consumerSecret', 'Consumer Secret', true],
    ['token', 'Token'],
    ['tokenSecret', 'Token Secret', true],
    ['signatureMethod', '签名方法（HMAC-SHA1/HMAC-SHA256）'],
  ],
  oauth2: [
    ['grantType', '授权模式（authorization_code/client_credentials/password）'],
    ['authUrl', '授权端点（authorization_code 用）'],
    ['tokenUrl', 'Token 端点'],
    ['clientId', 'Client ID'],
    ['clientSecret', 'Client Secret', true],
    ['scope', 'Scope（可选）'],
    ['username', '用户名（password 模式）'],
    ['password', '密码（password 模式）', true],
  ],
  awsv4: [
    ['accessKey', 'Access Key'],
    ['secretKey', 'Secret Key', true],
    ['region', 'Region（默认 us-east-1）'],
    ['service', 'Service（默认 execute-api）'],
    ['sessionToken', 'Session Token（可选）', true],
  ],
};

interface Props {
  auth: Auth;
  onChange(auth: Auth): void;
}

export default function AuthEditor({ auth, onChange }: Props) {
  const type = auth?.type ?? 'inherit';
  const params = auth?.params ?? {};
  const fields = FIELDS[type] ?? [];
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
        <label className="text-sm text-gray-600 w-20">认证类型</label>
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
              {label}
            </option>
          ))}
        </select>
      </div>
      {fields.map(([key, label, secret]) => (
        <div key={key} className="flex items-center gap-2">
          <label className="text-sm text-gray-600 w-20 shrink-0">{label.split('（')[0]}</label>
          <input
            className="border rounded px-2 py-1 text-sm flex-1 font-mono"
            type={secret ? 'password' : 'text'}
            placeholder={label.includes('（') ? label.slice(label.indexOf('（') + 1, -1) : ''}
            value={params[key] ?? ''}
            onChange={(e) =>
              onChange({ type, params: { ...params, [key]: e.target.value } } as Auth)
            }
          />
        </div>
      ))}
      {type === 'inherit' && (
        <p className="text-xs text-gray-400">使用最近一级集合/文件夹上配置的认证。</p>
      )}
      {type === 'oauth2' && (
        <div className="space-y-2 pt-1">
          <div className="flex items-center gap-2">
            <button
              className="bg-blue-600 text-white rounded px-3 py-1 text-sm hover:bg-blue-700 disabled:opacity-50"
              disabled={tokenState === 'fetching' || !params.tokenUrl}
              onClick={fetchToken}
            >
              {tokenState === 'fetching' ? '获取中…' : '获取 Token'}
            </button>
            {params.accessToken && (
              <>
                <span className="text-xs text-green-600 font-mono">
                  <Verbatim value={params.accessToken.slice(0, 24)} />…
                </span>
                <button className="text-xs text-red-500 hover:underline" onClick={clearToken}>
                  清除
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
        <p className="text-xs text-gray-400">{'支持 {{变量}} 占位，发送时解析。'}</p>
      )}
    </div>
  );
}
