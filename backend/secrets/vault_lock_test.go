package secrets

import "testing"

// Lock() 的语义是"从内存清除解密内容"。它必须同时清 knownValues 与 knownOrder：
// 只清前者会留下两个后果 —— 明文残留在 knownOrder 里，且 FIFO 淘汰会去删
// 已不存在的 key，使 knownValues 突破 maxKnownValues 上限继续增长。
func TestLockClearsRedactionCacheCompletely(t *testing.T) {
	v := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	for _, s := range []string{"secret-a", "secret-b", "secret-c"} {
		v.mu.Lock()
		v.remember(s)
		v.mu.Unlock()
	}

	v.Lock()

	v.mu.RLock()
	values, order := len(v.knownValues), len(v.knownOrder)
	v.mu.RUnlock()
	if values != 0 {
		t.Errorf("knownValues = %d after Lock, want 0", values)
	}
	if order != 0 {
		t.Errorf("knownOrder = %d after Lock, want 0 (plaintext retained in memory)", order)
	}
}

// Lock 之后重新灌满，缓存不得突破上限（失同步会让淘汰删错对象）
func TestRedactionCacheStaysBoundedAfterLock(t *testing.T) {
	v := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	v.mu.Lock()
	for i := 0; i < 8; i++ {
		v.remember(string(rune('a'+i)) + "-pre-lock")
	}
	v.mu.Unlock()

	v.Lock()

	v.mu.Lock()
	for i := 0; i < maxKnownValues+16; i++ {
		v.remember("refill-" + string(rune(i%26+'a')) + "-" + itoa(i))
	}
	values, order := len(v.knownValues), len(v.knownOrder)
	v.mu.Unlock()

	if values > maxKnownValues {
		t.Errorf("knownValues = %d, exceeds cap %d", values, maxKnownValues)
	}
	if order > maxKnownValues {
		t.Errorf("knownOrder = %d, exceeds cap %d (unbounded growth)", order, maxKnownValues)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
