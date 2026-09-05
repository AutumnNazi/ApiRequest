package binding

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// allowQuitTTL 退出票据有效期：票据置位后未在窗口内消费即作废，
// 防止 Quit 未触发 BeforeClose 时票据被下一次原生关闭"错放"（绕过脏草稿守卫）
const allowQuitTTL = 5 * time.Second

// LifecycleApi funnels native and custom-titlebar close requests through the
// same frontend dirty-draft guard before allowing the application to quit.
type LifecycleApi struct {
	ctx        context.Context
	allowQuit  atomic.Bool
	quitIssued atomic.Int64 // Unix ms，0 = 无待消费票据
}

func NewLifecycleApi() *LifecycleApi { return &LifecycleApi{} }

func (a *LifecycleApi) startup(ctx context.Context) { a.ctx = ctx }

func (a *LifecycleApi) consumeAllowQuit() bool {
	issued := a.quitIssued.Load()
	if issued == 0 {
		return false
	}
	// 过期票据作废
	if time.Now().UnixMilli()-issued > allowQuitTTL.Milliseconds() {
		a.quitIssued.Store(0)
		return false
	}
	a.quitIssued.Store(0)
	return a.allowQuit.Swap(false)
}

// BeforeClose is the native Wails callback. It is a package function so Wails
// does not expose a context.Context argument as a frontend binding.
func BeforeClose(a *LifecycleApi, ctx context.Context) bool {
	if a.consumeAllowQuit() {
		return false
	}
	wailsrt.EventsEmit(ctx, "app:close-request")
	return true
}

// RequestQuit allows exactly one native close after the frontend has confirmed
// and synchronously flushed its recoverable drafts.
func (a *LifecycleApi) RequestQuit() error {
	if a.ctx == nil {
		return errors.New("application lifecycle is not available before startup")
	}
	a.allowQuit.Store(true)
	a.quitIssued.Store(time.Now().UnixMilli())
	wailsrt.Quit(a.ctx)
	return nil
}
