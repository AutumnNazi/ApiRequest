package auth

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strings"
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
	cnonce := randomHex(16)
	if qop == "auth" {
		response = h(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = h(ha1 + ":" + nonce + ":" + ha2)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`,
		username, realm, nonce, uri, response)
	if qop == "auth" {
		fmt.Fprintf(&b, `, qop=auth, nc=%s, cnonce=%q`, nc, cnonce)
	}
	if ch["opaque"] != "" {
		fmt.Fprintf(&b, `, opaque=%q`, ch["opaque"])
	}
	if ch["algorithm"] != "" {
		fmt.Fprintf(&b, `, algorithm=%s`, ch["algorithm"])
	}
	req.Header.Set("Authorization", b.String())
	return true, nil
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

func randomHex(n int) string {
	b := make([]byte, n/2)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func init() { Register(digestAuth{}) }
