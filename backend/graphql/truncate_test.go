package graphql

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// truncate 的输入是任意 HTTP 响应体，可能本身就不是合法 UTF-8。
// 回归：末尾边界回退曾用整串 ValidString 判定，遇到二进制内容会几乎删空。
func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("under limit = %q, want unchanged", got)
	}

	// 多字节字符被切半时只回退末尾残字节
	cn := strings.Repeat("拒", 10) // 每字符 3 字节
	got := truncate(cn, 10)
	if !utf8.ValidString(strings.TrimSuffix(got, "…")) {
		t.Errorf("cn truncate left invalid UTF-8: %q", got)
	}
	if want := "拒拒拒…"; got != want {
		t.Errorf("cn truncate = %q, want %q", got, want)
	}

	// 输入含无效字节时仍须保留约 n 字节内容，不能整体塌缩
	binary := "ERROR: " + strings.Repeat("\xff\xfe", 20)
	out := truncate(binary, 30)
	if body := strings.TrimSuffix(out, "…"); len(body) < 25 {
		t.Errorf("binary truncate kept only %d bytes (%q), want ~30", len(body), out)
	}
}

// 二进制/含无效字节的输入：截断不得把内容剥空（只回退末尾被切半的序列）
func TestTruncateKeepsBinaryContent(t *testing.T) {
	// 混合输入：ASCII + 控制字节 + 多字节字符 + 大段无效字节尾巴。
	// 必须显著长于 n，否则 truncate 的 len(s) <= n 提前返回，截断逻辑一行都不跑
	in := "abc\x01\x02def" + strings.Repeat("中", 4) + strings.Repeat("\xff\xfe\xfd", 10) + "tail"
	const n = 30
	if len(in) <= n {
		t.Fatalf("test input len = %d, must exceed n = %d to exercise truncation", len(in), n)
	}
	out := truncate(in, n)
	body := strings.TrimSuffix(out, "…")
	// 整串 ValidString 回退会一路剥到首个坏字节之前，把内容几乎删空
	if len(body) < n-utf8.UTFMax {
		t.Errorf("kept only %d bytes (%q), want close to %d", len(body), out, n)
	}
	if !strings.HasPrefix(out, "abc") {
		t.Fatalf("prefix lost: %q", out)
	}
}

// CJK 字符恰被切断：回退到字符边界，不残留半个字符
func TestTruncateCutsOnRuneBoundary(t *testing.T) {
	// n 必须不是 3 的倍数才会切在"中"（3 字节）中间：n=30 时 30%3==0，
	// 切点恰好落在字符边界上，回退循环第一次迭代就 break，测不到任何东西
	in := strings.Repeat("中", 20) // 60 字节
	const n = 31                  // 切在第 11 个"中"的首字节之后，残留 1 个字节
	if n%3 == 0 {
		t.Fatalf("n = %d lands on a rune boundary; pick one that splits a 3-byte rune", n)
	}
	out := truncate(in, n)
	for _, r := range out {
		if r == utf8.RuneError {
			t.Fatalf("output contains RuneError: %q", out)
		}
	}
	// 回退掉半个字符后应恰好剩 10 个完整的"中"
	if want := strings.Repeat("中", 10) + "…"; out != want {
		t.Fatalf("boundary wrong: got %q, want %q", out, want)
	}
}
