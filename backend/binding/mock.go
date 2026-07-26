package binding

import (
	"context"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/mock"
	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// ExampleApi 示例域（"保存为示例" + Mock 数据源）
type ExampleApi struct {
	store *storage.Store
}

// NewExampleApi 构造
func NewExampleApi(store *storage.Store) *ExampleApi { return &ExampleApi{store: store} }

// ListExamples 列出请求的示例
func (a *ExampleApi) ListExamples(nodeId string) ([]model.Example, error) {
	out, err := a.store.ListExamples(nodeId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// UpsertExample 保存示例
func (a *ExampleApi) UpsertExample(e model.Example) (model.Example, error) {
	if e.NodeId == "" || e.Name == "" {
		return e, model.NewError(model.KindValidation, "example nodeId and name are required")
	}
	out, err := a.store.UpsertExample(e)
	if err != nil {
		return out, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// DeleteExample 删除示例
func (a *ExampleApi) DeleteExample(exampleId string) error {
	if err := a.store.DeleteExample(exampleId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// MockApi Mock Server 域
type MockApi struct {
	ctx     context.Context
	store   *storage.Store
	manager *mock.Manager
}

// NewMockApi 构造
func NewMockApi(store *storage.Store, manager *mock.Manager) *MockApi {
	return &MockApi{store: store, manager: manager}
}

func (a *MockApi) startup(ctx context.Context) { a.ctx = ctx }

// mockStatus mock:status 事件负载
type mockStatus struct {
	CollectionId string `json:"collectionId"`
	State        string `json:"state"` // running | stopped
	Addr         string `json:"addr,omitempty"`
}

// mockLog mock:log 事件负载
type mockLog struct {
	CollectionId string `json:"collectionId"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	Matched      string `json:"matched,omitempty"`
	Status       int    `json:"status"`
	Ts           int64  `json:"ts"`
}

// MockStatus 启动结果
type MockStatus struct {
	CollectionId string `json:"collectionId"`
	Addr         string `json:"addr"`
	Routes       int    `json:"routes"`
}

// StartMockServer 启动集合的 mock
func (a *MockApi) StartMockServer(collectionId string, opts mock.Options) (MockStatus, error) {
	chain, err := a.store.NodeAncestors(collectionId)
	if err != nil || len(chain) == 0 {
		return MockStatus{}, model.NewError(model.KindStorage, "collection not found: "+collectionId)
	}
	root := chain[0]
	all, err := a.store.ListNodes(root.WorkspaceId)
	if err != nil {
		return MockStatus{}, model.WrapError(model.KindStorage, err)
	}
	// 收集集合子树节点
	inTree := map[string]bool{collectionId: true}
	changed := true
	for changed {
		changed = false
		for _, n := range all {
			if !inTree[n.Id] && inTree[n.ParentId] {
				inTree[n.Id] = true
				changed = true
			}
		}
	}
	var nodes []model.Node
	for _, n := range all {
		if inTree[n.Id] {
			nodes = append(nodes, n)
		}
	}

	examples, err := a.store.ListExamplesForCollection(collectionId)
	if err != nil {
		return MockStatus{}, model.WrapError(model.KindStorage, err)
	}

	onLog := func(method, path, matched string, status int) {
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, "mock:log", mockLog{
				CollectionId: collectionId, Method: method, Path: path,
				Matched: matched, Status: status, Ts: nowUnixMs(),
			})
		}
	}
	srv, err := a.manager.Start(collectionId, nodes, examples, opts, onLog)
	if err != nil {
		return MockStatus{}, err
	}
	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "mock:status", mockStatus{
			CollectionId: collectionId, State: "running", Addr: srv.Addr,
		})
	}
	return MockStatus{CollectionId: collectionId, Addr: srv.Addr, Routes: len(examples)}, nil
}

// StopMockServer 停止集合的 mock
func (a *MockApi) StopMockServer(collectionId string) error {
	a.manager.Stop(collectionId)
	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "mock:status", mockStatus{CollectionId: collectionId, State: "stopped"})
	}
	return nil
}

// RunningMocks 返回运行中的 mock（collectionId → 地址）
func (a *MockApi) RunningMocks() map[string]string {
	return a.manager.Running()
}
