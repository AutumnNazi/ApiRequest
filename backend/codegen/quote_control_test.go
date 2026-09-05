package codegen

import (
	"strings"
	"testing"
)

// HTTP 头值可含任意控制字节。各语言必须用自己"还原为同一控制字符"的原生
// 转义形式输出：\u0001（翻倍反斜杠）不算通过——那在目标语言里是 6 字节
// 字面文本而非控制字符，正是"转义先于反斜杠翻倍执行"这一顺序缺陷的指纹。
// Rust 是花括号形式 \u{1}（\u0001 在 Rust 里是语法错误）；PHP 单引号串没有
// 控制字符转义，须断链拼一段双引号 "\x01"（那里有 \xHH）。
func TestQuotersEscapeControlChars(t *testing.T) {
	const in = "a\x01b\x1fc"
	want := map[string]string{
		"jsonQuote":   `"a\u0001b\u001fc"`,
		"javaQuote":   `"a\u0001b\u001fc"`,
		"rustQuote":   `"a\u{1}b\u{1f}c"`,
		"phpQuote":    `'a'."\x01".'b'."\x1f".'c'`,
		"csharpQuote": `"a\u0001b\u001fc"`,
		"pyQuote":     `"a\u0001b\u001fc"`,
		"goQuote":     `"a\u0001b\u001fc"`,
	}
	quoters := map[string]func(string) string{
		"jsonQuote":   jsonQuote,
		"javaQuote":   javaQuote,
		"rustQuote":   rustQuote,
		"phpQuote":    phpQuote,
		"csharpQuote": csharpQuote,
		"pyQuote":     pyQuote,
		"goQuote":     goQuote,
	}
	for name, q := range quoters {
		if out := q(in); out != want[name] {
			t.Errorf("%s(%q) = %q, want %q", name, in, out, want[name])
		}
	}
}

// \r \n \t 有各语言的原生转义，不应被改写成 \uXXXX
func TestQuotersKeepNativeEscapesForCommonWhitespace(t *testing.T) {
	out := goQuote("a\nb\tc")
	if strings.Contains(out, `\u000a`) || strings.Contains(out, `\u0009`) {
		t.Errorf("goQuote = %q, want native \n and \t escapes", out)
	}
}
