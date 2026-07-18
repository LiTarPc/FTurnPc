//go:build linux

package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

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

func main() {
	backend.Init(deployScript, serverBinary)
	app := backend.NewApp(trayIcon)

	err := wails.Run(&options.App{
		Title:     "FTurnPc",
		Width:     380,
		Height:    800,
		MinWidth:  420,
		MinHeight: 350,
		Frameless: false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup:        app.Startup,
		OnBeforeClose:    app.OnBeforeClose,
		Bind:             []interface{}{app},
		Linux: &linux.Options{
			ProgramName: "FTurnPc",
			Icon:        appIcon,
		},
	})
	if err != nil {
		panic(err)
	}
}
