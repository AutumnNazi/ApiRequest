package script

import (
	"errors"
	"strings"
	"testing"

	"apirequest/backend/model"
)

// panic 兜底路径不得重复执行 onFinish：曾经 recover 分支从头重跑整个列表，
// 正常路径已跑过的钩子会被跑第二次（变量变更被重复提交）。
func TestFinishHooksRunExactlyOnceOnPanic(t *testing.T) {
	s := newTestSandbox()
	calls := 0
	s.onFinish = append(s.onFinish, func() { calls++ })

	// SendFunc 里制造真实 Go panic（nil map 写入），触发 Run 的 recover 兜底
	s.SendFunc = func(model.HttpRequest) (model.ResponseResult, error) {
		var broken map[string]string
		broken["boom"] = "panic"
		return model.ResponseResult{}, nil
	}

	err := s.Run(`pm.sendRequest('https://example.test', function () {})`, "pre")
	if err == nil {
		t.Fatal("expected panic to surface as a script error")
	}
	if ae := (*model.AppError)(nil); !errors.As(err, &ae) || ae.Kind != model.KindScript {
		t.Fatalf("err = %#v, want KindScript AppError", err)
	}
	if calls != 1 {
		t.Errorf("onFinish ran %d times, want exactly 1", calls)
	}
}

// 钩子自身 panic 时必须被逐个隔离：既不能逃出去打崩进程，
// 也不能中断后续钩子（否则一个坏钩子会吞掉其余变量变更）。
func TestPanickingFinishHookIsIsolated(t *testing.T) {
	s := newTestSandbox()
	later := false
	s.onFinish = append(s.onFinish,
		func() { panic("hook exploded") },
		func() { later = true },
	)

	if err := s.Run(`pm.environment.set('k', 'v')`, "pre"); err != nil {
		t.Fatalf("a panicking finish hook must not fail the run: %v", err)
	}
	if !later {
		t.Error("a panicking hook aborted the remaining hooks")
	}
	if got := s.Result().EnvChanges.Set["k"]; got != "v" {
		t.Errorf("env change lost: got %q, want \"v\"", got)
	}
}

// 正常路径同样只跑一次，且钩子清空后重复 Run 不会再触发旧钩子
func TestFinishHooksClearedAfterRun(t *testing.T) {
	s := newTestSandbox()
	calls := 0
	s.onFinish = append(s.onFinish, func() { calls++ })

	if err := s.Run(`1 + 1`, "pre"); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(`1 + 1`, "pre"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("onFinish ran %d times across two runs, want 1", calls)
	}
}

// 脚本抛错（非 panic）时钩子也应执行，保证变量变更不丢
func TestFinishHooksRunWhenScriptThrows(t *testing.T) {
	s := newTestSandbox()
	err := s.Run(`pm.environment.set('k', 'v'); throw new Error('boom')`, "pre")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want the thrown error", err)
	}
	if got := s.Result().EnvChanges.Set["k"]; got != "v" {
		t.Errorf("env change lost on throw: got %q, want \"v\"", got)
	}
}
