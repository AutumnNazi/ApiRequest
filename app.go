package main

import (
	"context"
	"log"
	"time"

	"apirequest/backend/binding"
	"apirequest/backend/grpcclient"
	"apirequest/backend/httpengine"
	"apirequest/backend/mock"
	"apirequest/backend/platform"
	"apirequest/backend/protocol"
	"apirequest/backend/storage"
)

// App 聚合各领域绑定与生命周期
type App struct {
	store     *storage.Store
	mocks     *mock.Manager
	protocols *protocol.Manager

	Request  *binding.RequestApi
	Node     *binding.NodeApi
	History  *binding.HistoryApi
	Env      *binding.EnvApi
	Cookie   *binding.CookieApi
	Convert  *binding.ConvertApi
	Runner   *binding.RunnerApi
	Example  *binding.ExampleApi
	Mock     *binding.MockApi
	Protocol *binding.ProtocolApi
	OAuth2   *binding.OAuth2Api
	Settings *binding.SettingsApi
	Grpc     *binding.GrpcApi
	Graphql  *binding.GraphqlApi
	Sync     *binding.SyncApi
	Dialog   *binding.DialogApi
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
	engine.SetBlobsDir(store.BlobsDir())
	mocks := mock.NewManager()
	protocols := protocol.NewManager(engine.NewHTTPClient(0))

	request := binding.NewRequestApi(engine, store)
	return &App{
		store:     store,
		mocks:     mocks,
		protocols: protocols,
		Request:   request,
		Node:      binding.NewNodeApi(store),
		History:   binding.NewHistoryApi(store),
		Env:       binding.NewEnvApi(store),
		Cookie:    binding.NewCookieApi(store),
		Convert:   binding.NewConvertApi(store),
		Runner:    binding.NewRunnerApi(request, store),
		Example:   binding.NewExampleApi(store),
		Mock:      binding.NewMockApi(store, mocks),
		Protocol:  binding.NewProtocolApi(protocols),
		OAuth2:    binding.NewOAuth2Api(engine.NewHTTPClient(30 * time.Second)),
		Settings:  binding.NewSettingsApi(store, engine),
		Grpc:      binding.NewGrpcApi(),
		Graphql:   binding.NewGraphqlApi(engine.NewHTTPClient(0)),
		Sync:      binding.NewSyncApi(store, engine),
		Dialog:    binding.NewDialogApi(),
	}
}

func (a *App) startup(ctx context.Context) {
	binding.Startup(ctx, a.Request, a.Runner, a.Mock, a.Protocol, a.OAuth2, a.Grpc, a.Graphql, a.Dialog)
}

func (a *App) shutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := binding.Shutdown(shutdownCtx, a.Runner, a.Request); err != nil {
		log.Printf("stop request operations: %v", err)
	}
	a.mocks.StopAll()
	a.protocols.CloseAll()
	grpcclient.CloseAllStreams()
	a.store.Close()
}
