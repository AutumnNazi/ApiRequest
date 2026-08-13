package binding

import (
	"context"
	"errors"
	"sync/atomic"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// LifecycleApi funnels native and custom-titlebar close requests through the
// same frontend dirty-draft guard before allowing the application to quit.
type LifecycleApi struct {
	ctx       context.Context
	allowQuit atomic.Bool
}

func NewLifecycleApi() *LifecycleApi { return &LifecycleApi{} }

func (a *LifecycleApi) startup(ctx context.Context) { a.ctx = ctx }

func (a *LifecycleApi) consumeAllowQuit() bool {
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
	wailsrt.Quit(a.ctx)
	return nil
}
