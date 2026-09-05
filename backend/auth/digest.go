package auth

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strings"

	"apirequest/backend/model"
)

// digestAuth RFC 7616 摘要认证（两段式：先 401 拿 nonce，再算摘要重发）。
// 支持 MD5 与 SHA-256、qop=auth。
type digestAuth struct{}

func (digestAuth) Type() string { return "digest" }

// Apply 首发不带凭证（等服务端 401 挑战）
func (digestAuth) Apply(req *http.Request, p map[string]string) error { return nil }

// OnChallenge 解析 WWW-Authenticate 并生成 Authorization
func (digestAuth) OnChallenge(req *http.Request, challenge string, p map[string]string) (bool, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return false, nil
	}
	ch := parseChallenge(challenge[len("Digest "):])
	realm, nonce := ch["realm"], ch["nonce"]
	if nonce == "" {
		return false, nil
	}
	algo := strings.ToUpper(ch["algorithm"])
	if algo == "" {
		algo = "MD5"
	}
	var newHash func() hash.Hash
	switch strings.TrimSuffix(algo, "-SESS") {
	case "MD5":
		newHash = md5.New
	case "SHA-256":
		newHash = sha256.New
	default:
		return false, fmt.Errorf("unsupported digest algorithm: %s", algo)
	}
	h := func(s string) string {
		hh := newHash()
		hh.Write([]byte(s))
		return hex.EncodeToString(hh.Sum(nil))
	}

	username, password := p["username"], p["password"]
	uri := req.URL.RequestURI()
	ha1 := h(username + ":" + realm + ":" + password)
	ha2 := h(req.Method + ":" + uri)

	var response string
	qop := ""
	// qop 可能是 "auth" 或 "auth, auth-int"，取 auth
	for _, q := range strings.Split(ch["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
			break
		}
	}
	nc := "00000001"
	cnonce, err := randomHex(16)
	if err != nil {
		return false, err
	}
	if qop == "auth" {
		response = h(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = h(ha1 + ":" + nonce + ":" + ha2)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `Digest username=%s, realm=%s, nonce=%s, uri=%s, response=%s`,
		quotedString(username), quotedString(realm), quotedString(nonce), quotedString(uri), quotedString(response))
	if qop == "auth" {
		fmt.Fprintf(&b, `, qop=auth, nc=%s, cnonce=%s`, nc, quotedString(cnonce))
	}
	if ch["opaque"] != "" {
		fmt.Fprintf(&b, `, opaque=%s`, quotedString(ch["opaque"]))
	}
	if ch["algorithm"] != "" {
		fmt.Fprintf(&b, `, algorithm=%s`, ch["algorithm"])
	}
	req.Header.Set("Authorization", b.String())
	return true, nil
}

// quotedString HTTP quoted-string 转义：只转义 \ 和 "（RFC 9110 §5.6.4）。
// 不能用 Go 的 %q——它会产生 \x41、\u 等 Go 转义，HTTP 头里非法。
// 控制字符（含 CR/LF）在 quoted-string 里没有合法表示，只能剔除：
// 保留它们会让 Authorization 头被 net/http 拒绝，CR/LF 更是头注入向量
func quotedString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		if c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// parseChallenge 解析 key="value", key=value 逗号分隔串
func parseChallenge(s string) map[string]string {
	out := map[string]string{}
	for _, part := range splitChallenge(s) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		out[k] = v
	}
	return out
}

// splitChallenge 按逗号分割，但忽略引号内的逗号
func splitChallenge(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// randomHex 生成 n 位十六进制随机串（cnonce）。
// 失败必须显式失败：忽略错误会让 b 保持全零，cnonce 变成可预测常量，
// 削弱 Digest 的重放防护。返回错误而非 panic：调用链 OnChallenge→engine.send
// 上没有 recover，panic 会打崩桌面进程、丢掉用户未保存的脏草稿——
// 单次请求降级即可，不值得整个应用陪葬
func randomHex(n int) (string, error) {
	b := make([]byte, n/2)
	if _, err := randRead(b); err != nil {
		return "", model.NewError(model.KindNetwork, "crypto/rand unavailable: "+err.Error())
	}
	return hex.EncodeToString(b), nil
}

func init() { Register(digestAuth{}) }
