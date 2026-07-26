package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// oauth1Auth OAuth 1.0a HMAC 签名（docs/auth.md：签名基串 + 参数排序 + 百分号编码）
type oauth1Auth struct{}

func (oauth1Auth) Type() string { return "oauth1" }

func (oauth1Auth) Apply(req *http.Request, p map[string]string) error {
	consumerKey := p["consumerKey"]
	consumerSecret := p["consumerSecret"]
	token := p["token"]
	tokenSecret := p["tokenSecret"]
	sigMethod := p["signatureMethod"]
	if sigMethod == "" {
		sigMethod = "HMAC-SHA1"
	}

	oauthParams := map[string]string{
		"oauth_consumer_key":     consumerKey,
		"oauth_nonce":            oauthNonce(),
		"oauth_signature_method": sigMethod,
		"oauth_timestamp":        strconv.FormatInt(time.Now().Unix(), 10),
		"oauth_version":          "1.0",
	}
	if token != "" {
		oauthParams["oauth_token"] = token
	}

	// 收集签名参数：oauth_* + query + urlencoded body（此处仅取 query，body 已编码为流）
	all := url.Values{}
	for k, v := range oauthParams {
		all.Set(k, v)
	}
	for k, vs := range req.URL.Query() {
		for _, v := range vs {
			all.Add(k, v)
		}
	}

	// 规范化参数串：按 key（再按 value）排序，RFC3986 百分号编码
	type pair struct{ k, v string }
	var pairs []pair
	for k, vs := range all {
		for _, v := range vs {
			pairs = append(pairs, pair{percentEncode(k), percentEncode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	var paramParts []string
	for _, pr := range pairs {
		paramParts = append(paramParts, pr.k+"="+pr.v)
	}
	paramStr := strings.Join(paramParts, "&")

	// 基串：METHOD&encode(baseURL)&encode(params)
	baseURL := req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	baseStr := strings.ToUpper(req.Method) + "&" + percentEncode(baseURL) + "&" + percentEncode(paramStr)

	signingKey := percentEncode(consumerSecret) + "&" + percentEncode(tokenSecret)
	var sig string
	switch sigMethod {
	case "HMAC-SHA256":
		mac := hmac.New(sha256.New, []byte(signingKey))
		mac.Write([]byte(baseStr))
		sig = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	case "PLAINTEXT":
		sig = signingKey
	default: // HMAC-SHA1
		mac := hmac.New(sha1.New, []byte(signingKey))
		mac.Write([]byte(baseStr))
		sig = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	oauthParams["oauth_signature"] = sig

	// Authorization: OAuth k="v", ...（按 key 排序保证稳定）
	keys := make([]string, 0, len(oauthParams))
	for k := range oauthParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var hdr []string
	for _, k := range keys {
		hdr = append(hdr, fmt.Sprintf(`%s="%s"`, k, percentEncode(oauthParams[k])))
	}
	req.Header.Set("Authorization", "OAuth "+strings.Join(hdr, ", "))
	return nil
}

// percentEncode RFC 3986（OAuth 1.0 要求，比 url.QueryEscape 严格）
func percentEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func oauthNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func init() { Register(oauth1Auth{}) }
