package binding

import "testing"

func TestLifecycleAllowQuitIsConsumedOnce(t *testing.T) {
	a := NewLifecycleApi()
	if a.consumeAllowQuit() {
		t.Fatal("quit should not be allowed before confirmation")
	}
	a.allowQuit.Store(true)
	if !a.consumeAllowQuit() {
		t.Fatal("confirmed quit was not allowed")
	}
	if a.consumeAllowQuit() {
		t.Fatal("quit allowance leaked into a later close request")
	}
}

func TestLifecycleRequestQuitRequiresStartup(t *testing.T) {
	if err := NewLifecycleApi().RequestQuit(); err == nil {
		t.Fatal("RequestQuit succeeded before startup")
	}
}
