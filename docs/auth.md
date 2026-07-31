# 认证（Auth）实现细节

[English](./en/auth.md) | 简体中文

Auth 以"类型 + 参数"建模，统一在**变量解析之后、构造原生请求时**应用（可读取已解析的变量）。可在集合/文件夹级配置并向下继承，请求级可覆盖或选择 `inherit`。

相关文档：[文档索引](./index.md) · [请求生命周期](./request-lifecycle.md)

---

## 1. 认证类型总览

| 类型 | 实现要点 |
|------|---------|
| No Auth | 不加任何凭证 |
| Basic | `Authorization: Basic base64(user:pass)` |
| Bearer | `Authorization: Bearer <token>` |
| API Key | 注入到 header 或 query（可选位置） |
| Digest | 需先发一次拿 `WWW-Authenticate` 的 nonce，再计算摘要重发（引擎内两段式） |
| OAuth 1.0 | HMAC-SHA1/SHA256 签名基串，参数排序 + 百分号编码，`Authorization` 头 |
| OAuth 2.0 | 见下方各授权模式；token 可缓存并自动刷新 |
| AWS SigV4 | 规范请求 → 待签串 → 派生签名密钥 → `Authorization` 头，含 `x-amz-date` |
| Hawk / NTLM | 后期按需 |

---

## 2. OAuth 2.0 授权码模式时序（关键难点）

```
用户点"获取 Token"
  → Go 起临时本地回调服务(127.0.0.1:随机端口)
  → 经 platform.open 打开系统浏览器到授权端点(带 state + PKCE code_challenge)
  → 用户在浏览器授权
  → 授权服务器重定向到 http://127.0.0.1:port/callback?code=...&state=...
  → 本地服务捕获 code，校验 state
  → 后端用 code + code_verifier 换 access_token / refresh_token
  → token 存入凭证存储；到期前用 refresh_token 静默刷新
```

- **PKCE** 默认开启（S256），提升公共客户端安全性。
- 支持模式：Authorization Code (+PKCE)、Client Credentials、Password（弃用中，仅兼容）、Implicit（弃用，仅兼容旧接口）。
- Token 缓存键 = `(auth配置指纹)`，避免每次发请求都重新授权。

---

## 3. 凭证安全

- 密钥类变量与 OAuth token 默认存入系统 keychain（Windows Credential Manager / macOS Keychain）；后端不可用时使用主密码加密回退，见 [ADR-013](./decisions.md#adr-013-密钥存储默认系统-keychain无可用时主密码加密回退已定)。
- 同步时可选择不上传密钥值（见[协作与同步](./sync.md)）。
- OAuth token 与 refresh token 不写入 SQLite、历史记录或明文集合文件。

详见[安全考量](./ops.md#1-安全考量)。
