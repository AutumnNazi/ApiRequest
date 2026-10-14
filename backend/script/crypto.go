package script

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/dop251/goja"
)

// injectCrypto 注入全局 crypto 对象（国内接口加签场景的高频算法子集，
// 语义与 CryptoJS/OpenSSL 对齐：hex 小写、标准 Base64、AES-ECB-PKCS7）。
// 约束与 pm 同级暴露（无 require），不提供流式与密钥派生——
// CryptoJS 的 OpenSSL 风格 passphrase 派生不属于本子集，key 为原始 UTF-8 字节。
func injectCrypto(vm *goja.Runtime) error {
	c := vm.NewObject()
	must := func(fn func(call goja.FunctionCall) (string, error)) func(call goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			out, err := fn(call)
			if err != nil {
				panic(vm.NewGoError(err))
			}
			return vm.ToValue(out)
		}
	}
	arg := func(call goja.FunctionCall, i int) string {
		if i < len(call.Arguments) {
			return call.Arguments[i].String()
		}
		return ""
	}
	c.Set("md5", must(func(call goja.FunctionCall) (string, error) {
		sum := md5.Sum([]byte(arg(call, 0)))
		return hex.EncodeToString(sum[:]), nil
	}))
	c.Set("sha1", must(func(call goja.FunctionCall) (string, error) {
		sum := sha1.Sum([]byte(arg(call, 0)))
		return hex.EncodeToString(sum[:]), nil
	}))
	c.Set("sha256", must(func(call goja.FunctionCall) (string, error) {
		sum := sha256.Sum256([]byte(arg(call, 0)))
		return hex.EncodeToString(sum[:]), nil
	}))
	c.Set("hmacSha256", must(func(call goja.FunctionCall) (string, error) {
		mac := hmac.New(sha256.New, []byte(arg(call, 1)))
		mac.Write([]byte(arg(call, 0)))
		return hex.EncodeToString(mac.Sum(nil)), nil
	}))
	c.Set("hmacSha1", must(func(call goja.FunctionCall) (string, error) {
		mac := hmac.New(sha1.New, []byte(arg(call, 1)))
		mac.Write([]byte(arg(call, 0)))
		return hex.EncodeToString(mac.Sum(nil)), nil
	}))
	c.Set("base64", must(func(call goja.FunctionCall) (string, error) {
		return base64.StdEncoding.EncodeToString([]byte(arg(call, 0))), nil
	}))
	c.Set("base64Decode", must(func(call goja.FunctionCall) (string, error) {
		decoded, err := base64.StdEncoding.DecodeString(arg(call, 0))
		if err != nil {
			// 容忍无填充形态
			decoded, err = base64.RawStdEncoding.DecodeString(arg(call, 0))
			if err != nil {
				return "", fmt.Errorf("base64Decode: %w", err)
			}
		}
		return string(decoded), nil
	}))
	// AES-ECB + PKCS7：key 为 16/24/32 字节 UTF-8 原始密钥（对应 AES-128/192/256），
	// 输出 Base64。ECB 在加签兼容场景常见，但不提供 IV/ CBC——需要时应走脚本外通道。
	c.Set("aesEncrypt", must(func(call goja.FunctionCall) (string, error) {
		return aesECB([]byte(arg(call, 0)), []byte(arg(call, 1)), true)
	}))
	c.Set("aesDecrypt", must(func(call goja.FunctionCall) (string, error) {
		return aesECB([]byte(arg(call, 0)), []byte(arg(call, 1)), false)
	}))
	c.Set("randomHex", must(func(call goja.FunctionCall) (string, error) {
		n := int(call.Argument(0).ToInteger())
		if n < 0 || n > 1024 {
			return "", fmt.Errorf("randomHex: byte size %d out of range", n)
		}
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("randomHex: %w", err)
		}
		return hex.EncodeToString(buf), nil
	}))
	return vm.Set("crypto", c)
}

// aesECB 加解密一体：encrypt=true 时明文入/密文 Base64 出，反之亦然。
func aesECB(data, key []byte, encrypt bool) (string, error) {
	switch len(key) {
	case 16, 24, 32:
	default:
		return "", fmt.Errorf("aes key must be 16/24/32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes: %w", err)
	}
	if !encrypt {
		cipherText, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return "", fmt.Errorf("aesDecrypt: %w", err)
		}
		if len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
			return "", fmt.Errorf("aesDecrypt: invalid ciphertext length %d", len(cipherText))
		}
		out := make([]byte, len(cipherText))
		cipher.NewCBCDecrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, cipherText)
		return string(pkcs7Unpad(out)), nil
	}
	padded := pkcs7Pad(data, aes.BlockSize)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+pad)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	return padded
}

func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > len(data) {
		return data
	}
	return data[:len(data)-pad]
}
