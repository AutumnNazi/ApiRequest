# Authentication (Auth) Implementation Details

English | [简体中文](../auth.md)

Auth is modeled as a type plus parameters. It is applied **after variable resolution, while constructing the native request**, so it can consume resolved values. Auth can be configured at the collection or folder level and inherited by descendants; a request can override it or select `inherit`.

Related: [Documentation Index](./index.md) · [Request Lifecycle](./request-lifecycle.md)

---

## 1. Authentication Types

| Type | Implementation Notes |
|------|----------------------|
| No Auth | Add no credentials |
| Basic | `Authorization: Basic base64(user:pass)` |
| Bearer | `Authorization: Bearer <token>` |
| API Key | Inject into either a header or query parameter |
| Digest | Send once to obtain the nonce from `WWW-Authenticate`, calculate the digest, then resend as a two-stage engine flow |
| OAuth 1.0 | HMAC-SHA1/SHA256 signature base string, parameter sorting and percent encoding, then an `Authorization` header |
| OAuth 2.0 | Use the grant flows below; tokens can be cached and refreshed automatically |
| AWS SigV4 | Canonical request -> string to sign -> derived signing key -> `Authorization` header, including `x-amz-date` |
| Hawk / NTLM | Add later as needed |

---

## 2. OAuth 2.0 Authorization Code Flow

```text
User clicks "Get Token"
  -> Go starts a temporary local callback server (127.0.0.1:random-port)
  -> platform.open opens the authorization endpoint in the system browser
     with state and a PKCE code_challenge
  -> User authorizes in the browser
  -> Authorization server redirects to
     http://127.0.0.1:port/callback?code=...&state=...
  -> Local server captures the code and validates state
  -> Backend exchanges code + code_verifier for access_token / refresh_token
  -> Tokens are stored in credential storage and silently refreshed before expiry
```

- Enable **PKCE** with S256 by default to protect public clients.
- Supported grants: Authorization Code (+PKCE), Client Credentials, Password (deprecated, compatibility only), and Implicit (deprecated, legacy compatibility only).
- Token cache key = `(auth configuration fingerprint)`, avoiding authorization on every request.

---

## 3. Credential Security

- Secret variables and OAuth tokens are stored in the system keychain by default (Windows Credential Manager / macOS Keychain). If the backend is unavailable, use master-password encryption as a fallback. See [ADR-013](./decisions.md#adr-013-use-the-system-keychain-by-default-with-master-password-encryption-as-fallback-accepted).
- Users may opt out of uploading secret values during synchronization. See [Collaboration and Synchronization](./sync.md).
- OAuth access and refresh tokens are never written to SQLite, request history, or plaintext collection files.

See [Security Considerations](./ops.md#1-security-considerations) for details.
