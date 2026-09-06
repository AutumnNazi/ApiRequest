// Package version 持有应用版本号：构建时经 ldflags 注入
// （wails.json / CI：-X apirequest/backend/version.Version=x.y.z），开发构建为 "dev"。
package version

var Version = "dev"
