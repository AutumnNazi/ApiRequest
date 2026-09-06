// updatetool 发布签名工具（ADR-018）：生成 ed25519 密钥对、对渠道 manifest 签名。
// 私钥只存在于发布机（GitHub Actions repository secret 注入），公钥随客户端内置。
//
// 用法：
//
//	updatetool keygen                       # 输出私钥/公钥（hex）
//	updatetool sign <manifest-file> <priv>  # 输出 base64 签名（写入 <file>.sig）
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		cmdKeygen()
	case "sign":
		if len(os.Args) != 4 {
			usage()
			os.Exit(2)
		}
		cmdSign(os.Args[2], os.Args[3])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `updatetool — update manifest signing tool

Usage:
  updatetool keygen
      Print a new ed25519 keypair (private/public, hex).

  updatetool sign <manifest-file> <private-key-hex>
      Sign the manifest bytes and write base64 signature to <manifest-file>.sig.
`)
}

func cmdKeygen() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate key:", err)
		os.Exit(2)
	}
	fmt.Printf("private: %s\npublic:  %s\n",
		hex.EncodeToString(priv), hex.EncodeToString(pub))
}

func cmdSign(manifestPath, privHex string) {
	priv, err := parsePrivateKey(privHex)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read manifest:", err)
		os.Exit(2)
	}
	sig := ed25519.Sign(priv, data)
	out := manifestPath + ".sig"
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write signature:", err)
		os.Exit(2)
	}
	fmt.Println("signed:", filepath.ToSlash(out))
}

func parsePrivateKey(privHex string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(privHex))
	if err != nil {
		return nil, fmt.Errorf("private key must be hex: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key size = %d, want %d", len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}
