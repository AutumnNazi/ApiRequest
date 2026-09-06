package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"apirequest/backend/binding"
	"apirequest/backend/grpcclient"
	"apirequest/backend/httpengine"
	"apirequest/backend/mock"
	"apirequest/backend/platform"
	"apirequest/backend/protocol"
	"apirequest/backend/storage"
	"apirequest/backend/updater"
)

// App 聚合各领域绑定与生命周期
type App struct {
	store     *storage.Store
	mocks     *mock.Manager
	protocols *protocol.Manager
	// 自动定时同步的停止函数；nil = 未启动
	stopAutoSync func()
	// 进程起点（init 时刻）：冷启动预算（ops.md §5 <1.5s）的测量锚点
	processStart time.Time

	Request   *binding.RequestApi
	Node      *binding.NodeApi
	History   *binding.HistoryApi
	Env       *binding.EnvApi
	Cookie    *binding.CookieApi
	Convert   *binding.ConvertApi
	Runner    *binding.RunnerApi
	Example   *binding.ExampleApi
	Mock      *binding.MockApi
	Protocol  *binding.ProtocolApi
	OAuth2    *binding.OAuth2Api
	Settings  *binding.SettingsApi
	Grpc      *binding.GrpcApi
	Graphql   *binding.GraphqlApi
	Sync      *binding.SyncApi
	Update    *binding.UpdateApi
	Dialog    *binding.DialogApi
	Lifecycle *binding.LifecycleApi
}

// NewApp 初始化 core：数据目录 → 存储 → 引擎 → 绑定
func NewApp() *App {
	app := &App{processStart: time.Now()}
	paths, err := platform.ResolvePaths()
	if err != nil {
		log.Fatalf("resolve application paths: %v", err)
	}
	store, err := storage.Open(paths.Data)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	engine := httpengine.New()
	engine.SetBlobsDir(paths.Blobs)
	mocks := mock.NewManager()
	protocols := protocol.NewManager(engine.NewHTTPClient(0))

	request := binding.NewRequestApi(engine, store)
	runner := binding.NewRunnerApi(request, store)
	lifecycle := binding.NewLifecycleApi()
	return &App{
		store:        store,
		mocks:        mocks,
		protocols:    protocols,
		processStart: app.processStart,
		Request:   request,
		Node:      binding.NewNodeApi(store, request),
		History:   binding.NewHistoryApi(store),
		Env:       binding.NewEnvApi(store),
		Cookie:    binding.NewCookieApi(store),
		Convert:   binding.NewConvertApi(store),
		Runner:    runner,
		Example:   binding.NewExampleApi(store),
		Mock:      binding.NewMockApi(store, mocks),
		Protocol:  binding.NewProtocolApi(protocols),
		OAuth2:    binding.NewOAuth2Api(store, engine.NewHTTPClient(30*time.Second)),
		Settings:  binding.NewSettingsApi(store, engine),
		Grpc:      binding.NewGrpcApi(),
		Graphql:   binding.NewGraphqlApi(engine.NewHTTPClient(0)),
		Sync:      binding.NewSyncApi(store, engine, binding.RequestOperations(request)),
		Update:    binding.NewUpdateApi(store),
		Dialog:    binding.NewDialogApi(),
		Lifecycle: lifecycle,
	}
}

func (a *App) startup(ctx context.Context) {
	binding.Startup(ctx, a.Request, a.Runner, a.Mock, a.Protocol, a.OAuth2, a.Grpc, a.Graphql, a.Sync, a.Dialog, a.Lifecycle)
	// 30s 检查一次是否到期；实际间隔由各工作区 intervalMinutes 决定
	a.stopAutoSync = a.Sync.StartAutoSync(30 * time.Second)
	// ADR-018：应用上一轮登记的 pending 更新（校验→备份→换入），失败记日志并回滚
	if err := updater.ApplyPendingUpdate(updaterExePath()); err != nil {
		log.Printf("apply pending update: %v", err)
	}
	// 每日滚动备份（24h 内已有快照则跳过）：异步执行，失败只记日志不阻塞启动
	go func() {
		paths, err := platform.ResolvePaths()
		if err != nil {
			log.Printf("auto backup: resolve paths: %v", err)
			return
		}
		if _, err := a.store.AutoBackup(filepath.Join(paths.Data, "backups"), 7); err != nil {
			log.Printf("auto backup: %v", err)
		}
	}()
}

// domReady 前端 DOM 就绪即记录冷启动耗时（预算 <1.5s，见 docs/ops.md §5），
// 数值进诊断包供报障分析；测量锚点是 NewApp 的进程起点
func (a *App) domReady(ctx context.Context) {
	a.Settings.RecordStartup(time.Since(a.processStart).Milliseconds())
}

func (a *App) beforeClose(ctx context.Context) bool { return binding.BeforeClose(a.Lifecycle, ctx) }

func (a *App) shutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := binding.Shutdown(shutdownCtx, a.Request); err != nil {
		log.Printf("stop request operations: %v", err)
	}
	if a.stopAutoSync != nil {
		a.stopAutoSync()
	}
	a.mocks.StopAll()
	a.protocols.CloseAll()
	grpcclient.CloseAllStreams()
	a.store.Close()
}

// updaterExePath 当前可执行文件路径（pending 更新的替换目标）
func updaterExePath() string {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("resolve executable: %v", err)
		return ""
	}
	return exe
}
