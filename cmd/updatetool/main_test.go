package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

// parsePrivateKey 是签名链路唯一的私钥输入闸：ed25519.Sign 对错长度的 key 直接 panic，
// 因此尺寸校验必须在这里挡住，不能放过去（发布脚本传错 secret 时应报错退出而非崩溃）。
func TestParsePrivateKeyAcceptsGeneratedKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// keygen 输出的是 hex，且发布脚本经环境变量传递时常带换行/空格
	parsed, err := parsePrivateKey("  " + hex.EncodeToString(priv) + "\n")
	if err != nil {
		t.Fatalf("parsePrivateKey: %v", err)
	}
	if !parsed.Equal(priv) {
		t.Fatal("parsed key differs from the generated key")
	}
	// 解析结果可直接签名（不 panic 即证明长度合法）
	if len(ed25519.Sign(parsed, []byte("manifest"))) != ed25519.SignatureSize {
		t.Fatal("signature size mismatch")
	}
}

func TestParsePrivateKeyRejectsBadInput(t *testing.T) {
	full := hex.EncodeToString(make([]byte, ed25519.PrivateKeySize))
	cases := []struct {
		name    string
		input   string
		wantSub string
	}{
		{"非 hex 字符", "zzzz", "hex"},
		{"空字符串按长度不足拒绝", "", "size"},
		// 32 字节是公钥长度：误把公钥当私钥传入是最容易犯的错，必须报错而不是 panic
		{"公钥长度", hex.EncodeToString(make([]byte, ed25519.PublicKeySize)), "size"},
		{"截断的私钥", full[:len(full)-2], "size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := parsePrivateKey(tc.input)
			if err == nil {
				t.Fatalf("parsePrivateKey(%q) = %v, want error", tc.input, key)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want mention of %q", err, tc.wantSub)
			}
			if key != nil {
				t.Fatal("error path must not return a key")
			}
		})
	}
}
