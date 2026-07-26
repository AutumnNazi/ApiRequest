// Auth 编辑：类型选择 + 按类型渲染参数表单
import type { Auth } from '../ipc';

const AUTH_TYPES: [string, string][] = [
  ['inherit', '继承父级'],
  ['none', 'No Auth'],
  ['basic', 'Basic Auth'],
  ['bearer', 'Bearer Token'],
  ['apikey', 'API Key'],
  ['digest', 'Digest'],
  ['oauth1', 'OAuth 1.0'],
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

  return (
    <div className="space-y-3 max-w-lg">
      <div className="flex items-center gap-2">
        <label className="text-sm text-gray-600 w-20">认证类型</label>
        <select
          className="border rounded px-2 py-1 text-sm flex-1"
          value={type}
          onChange={(e) => onChange({ type: e.target.value, params: {} } as Auth)}
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
      {fields.length > 0 && (
        <p className="text-xs text-gray-400">支持 {'{{变量}}'} 占位，发送时解析。</p>
      )}
    </div>
  );
}
