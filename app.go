package main

import (
	"context"
	"log"

	"apirequest/backend/binding"
	"apirequest/backend/httpengine"
	"apirequest/backend/platform"
	"apirequest/backend/storage"
)

// App 聚合各领域绑定与生命周期
type App struct {
	store *storage.Store

	Request *binding.RequestApi
	Node    *binding.NodeApi
	History *binding.HistoryApi
	Env     *binding.EnvApi
}

// NewApp 初始化 core：数据目录 → 存储 → 引擎 → 绑定
func NewApp() *App {
	dataDir, err := platform.DataDir()
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}
	store, err := storage.Open(dataDir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	engine := httpengine.New()
	return &App{
		store:   store,
		Request: binding.NewRequestApi(engine, store),
		Node:    binding.NewNodeApi(store),
		History: binding.NewHistoryApi(store),
		Env:     binding.NewEnvApi(store),
	}
}

func (a *App) startup(ctx context.Context) {
	binding.Startup(ctx, a.Request)
}

func (a *App) shutdown(ctx context.Context) {
	a.store.Close()
}
