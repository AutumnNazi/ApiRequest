package binding

import (
	"testing"
	"time"
)

func TestLifecycleAllowQuitIsConsumedOnce(t *testing.T) {
	a := NewLifecycleApi()
	if a.consumeAllowQuit() {
		t.Fatal("quit should not be allowed before confirmation")
	}
	// 走真实路径：置位票据带时间戳（直接 Store allowQuit 不写 quitIssued 会被判无效）
	a.allowQuit.Store(true)
	a.quitIssued.Store(time.Now().UnixMilli())
	if !a.consumeAllowQuit() {
		t.Fatal("confirmed quit was not allowed")
	}
	if a.consumeAllowQuit() {
		t.Fatal("quit allowance leaked into a later close request")
	}
}

func TestLifecycleStaleQuitTicketExpires(t *testing.T) {
	a := NewLifecycleApi()
	// 超时未消费的票据应作废，不得被下一次原生关闭"错放"
	a.allowQuit.Store(true)
	a.quitIssued.Store(time.Now().Add(-time.Hour).UnixMilli())
	if a.consumeAllowQuit() {
		t.Fatal("stale quit ticket must not allow a later close to bypass the dirty-draft guard")
	}
}

func TestLifecycleRequestQuitRequiresStartup(t *testing.T) {
	if err := NewLifecycleApi().RequestQuit(); err == nil {
		t.Fatal("RequestQuit succeeded before startup")
	}
}
