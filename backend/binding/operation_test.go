package binding

import (
	"context"
	"fmt"
	"testing"
	"time"

	"apirequest/backend/runner"
)

func TestOperationRegistryIsSharedAcrossRequestAndRunner(t *testing.T) {
	request := NewRequestApi(nil, nil)
	runner := NewRunnerApi(request, nil)

	_, finish, err := request.operations.begin(context.Background(), "shared-id", "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if _, _, err := runner.operations.begin(context.Background(), "shared-id", "workspace-1"); err == nil {
		t.Fatal("duplicate operation id was accepted by another operation owner")
	}
}

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

func TestRunnerApiEvictsOldReports(t *testing.T) {
	api := NewRunnerApi(nil, nil)
	for index := 0; index <= maxCachedRunnerReports; index++ {
		id := fmt.Sprintf("run-%d", index)
		api.rememberReport(&runner.Report{RunId: id})
	}

	if _, err := api.ExportReport("run-0"); err == nil {
		t.Fatal("oldest runner report was not evicted")
	}
	latest := fmt.Sprintf("run-%d", maxCachedRunnerReports)
	if _, err := api.ExportReport(latest); err != nil {
		t.Fatalf("latest runner report was evicted: %v", err)
	}
	if len(api.reports) != maxCachedRunnerReports {
		t.Fatalf("cached report count = %d, want %d", len(api.reports), maxCachedRunnerReports)
	}
}
