package binding

import (
	"context"
	"testing"
	"time"
)

func TestOperationRegistryBlocksAndResumesScope(t *testing.T) {
	registry := newOperationRegistry()
	ctx, finish, err := registry.begin(context.Background(), "first", "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() {
		drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		drained <- registry.cancelScope(drainCtx, "workspace-1")
	}()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("scope cancellation did not reach the active operation")
	}
	if _, _, err := registry.begin(context.Background(), "blocked", "workspace-1"); err == nil {
		t.Fatal("new operation entered a blocked scope")
	}
	finish()
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	registry.resumeScope("workspace-1")
	_, resumedFinish, err := registry.begin(context.Background(), "resumed", "workspace-1")
	if err != nil {
		t.Fatalf("resumed scope rejected operation: %v", err)
	}
	resumedFinish()
}
