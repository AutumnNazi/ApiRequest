export type AuthField = readonly [key: string, label: string, secret?: boolean];

export const AUTH_TYPES: readonly (readonly [type: string, label: string])[] = [
  ['none', 'No Auth'],
  ['basic', 'Basic Auth'],
  ['bearer', 'Bearer Token'],
  ['apikey', 'API Key'],
  ['digest', 'Digest'],
  ['oauth1', 'OAuth 1.0'],
  ['oauth2', 'OAuth 2.0'],
  ['awsv4', 'AWS Signature V4'],
];

export const AUTH_FIELDS: Readonly<Record<string, readonly AuthField[]>> = {
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
    ['token', 'Token', true],
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

const normalizeKey = (key: string) => key.toLowerCase().replace(/[_\-\s]/g, '');

const sensitiveAuthParams = Object.fromEntries(
  Object.entries(AUTH_FIELDS).map(([type, fields]) => [
    type,
    new Set(fields.filter(([, , secret]) => secret).map(([key]) => normalizeKey(key))),
  ]),
) as Record<string, Set<string>>;

// OAuth2 tokens are obtained at runtime and are not rendered as editable fields.
sensitiveAuthParams.oauth2.add('accesstoken').add('refreshtoken');

export function shouldOmitAuthParam(authType: string | undefined, key: string): boolean {
  const sensitive = sensitiveAuthParams[authType?.toLowerCase() ?? ''];
  return !sensitive || sensitive.has(normalizeKey(key));
}

export function isSensitiveRequestValueKey(key: string): boolean {
  return /authorization|cookie|token|secret|api[-_]?key|password|passwd/i.test(key.trim());
}
