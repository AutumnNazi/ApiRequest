package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const maxBufferedSigV4Body = 8 << 20

// awsSigV4 AWS Signature Version 4（docs/auth.md：规范请求 → 待签串 → 派生密钥 → 签名）。
// params: accessKey / secretKey / region / service / sessionToken(可选)
type awsSigV4 struct{}

func (awsSigV4) Type() string { return "awsv4" }

func (awsSigV4) Apply(req *http.Request, p map[string]string) error {
	accessKey, secretKey := p["accessKey"], p["secretKey"]
	region, service := p["region"], p["service"]
	if region == "" {
		region = "us-east-1"
	}
	if service == "" {
		service = "execute-api"
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	// Hash replayable or seekable bodies as a stream. Only opaque one-shot
	// readers need bounded buffering so signing cannot consume unbounded memory.
	payloadHash, err := hashRequestPayload(req)
	if err != nil {
		return err
	}

	req.Header.Set("X-Amz-Date", amzDate)
	if p["sessionToken"] != "" {
		req.Header.Set("X-Amz-Security-Token", p["sessionToken"])
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}

	// 1. 规范请求
	signedHeaderKeys := []string{}
	canonHeaders := map[string]string{}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		// 仅签 host 与 x-amz-*、content-type（Postman 同款范围，避免代理改头破坏签名）
		if lk == "host" || lk == "content-type" || strings.HasPrefix(lk, "x-amz-") {
			signedHeaderKeys = append(signedHeaderKeys, lk)
			canonHeaders[lk] = strings.TrimSpace(strings.Join(vs, ","))
		}
	}
	if _, ok := canonHeaders["host"]; !ok {
		signedHeaderKeys = append(signedHeaderKeys, "host")
		canonHeaders["host"] = req.URL.Host
	}
	sort.Strings(signedHeaderKeys)

	var chBuilder strings.Builder
	for _, k := range signedHeaderKeys {
		chBuilder.WriteString(k + ":" + canonHeaders[k] + "\n")
	}
	signedHeaders := strings.Join(signedHeaderKeys, ";")

	// 规范 query：按 key 排序
	q := req.URL.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var qParts []string
	for _, k := range keys {
		vs := q[k]
		sort.Strings(vs)
		for _, v := range vs {
			qParts = append(qParts, percentEncode(k)+"="+percentEncode(v))
		}
	}

	canonPath := req.URL.EscapedPath()
	if canonPath == "" {
		canonPath = "/"
	}
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonPath,
		strings.Join(qParts, "&"),
		chBuilder.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	// 2. 待签串
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")

	// 3. 派生签名密钥
	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")

	// 4. 签名 + Authorization
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders, signature))
	return nil
}

func hashRequestPayload(req *http.Request) (string, error) {
	if req.Body == nil {
		return sha256Hex(""), nil
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return "", fmt.Errorf("reopen AWS SigV4 request body: %w", err)
		}
		hash, hashErr := sha256Reader(body)
		closeErr := body.Close()
		if hashErr != nil {
			return "", hashErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return hash, nil
	}
	if seeker, ok := req.Body.(io.Seeker); ok {
		position, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return "", fmt.Errorf("locate AWS SigV4 request body: %w", err)
		}
		hash, hashErr := sha256Reader(req.Body)
		_, seekErr := seeker.Seek(position, io.SeekStart)
		if hashErr != nil {
			return "", hashErr
		}
		if seekErr != nil {
			return "", fmt.Errorf("rewind AWS SigV4 request body: %w", seekErr)
		}
		return hash, nil
	}

	data, err := io.ReadAll(io.LimitReader(req.Body, maxBufferedSigV4Body+1))
	closeErr := req.Body.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(data) > maxBufferedSigV4Body {
		return "", fmt.Errorf("non-replayable AWS SigV4 request body exceeds %d MiB limit", maxBufferedSigV4Body>>20)
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return sha256Bytes(data), nil
}

func sha256Reader(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sha256Bytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func sha256Hex(s string) string {
	return sha256Bytes([]byte(s))
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func init() { Register(awsSigV4{}) }
