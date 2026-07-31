package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "ApiRequest",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 250, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app.Request,
			app.Node,
			app.History,
			app.Env,
			app.Cookie,
			app.Convert,
			app.Runner,
			app.Example,
			app.Mock,
			app.Protocol,
			app.OAuth2,
			app.Settings,
			app.Grpc,
			app.Graphql,
			app.Sync,
			app.Dialog,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
