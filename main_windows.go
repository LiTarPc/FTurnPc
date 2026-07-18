package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"pwdtt-desktop/backend"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/server/deploy.sh
var deployScript []byte

//go:embed assets/server/wdtt-server
var serverBinary []byte

//go:embed assets/icons/icon.png
var appIcon []byte

//go:embed assets/icons/tree-icon.png
var trayIcon []byte

//go:embed assets/wintun.dll
var wintunDLL []byte

func main() {

	backend.Init(deployScript, serverBinary)
	backend.InitWintun(wintunDLL)
	app := backend.NewApp(trayIcon)

	err := wails.Run(&options.App{
		Title:     "FTurnPc",
		Width:     400,
		Height:    420,
		MinWidth:  420,
		MinHeight: 300,
		Frameless: false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		OnStartup:        app.Startup,
		OnBeforeClose:    app.OnBeforeClose,
		Bind:             []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
		},
	})
	if err != nil {
		panic(err)
	}
}
