//go:build linux && !cgo

package tray

// Stub file for Linux when CGO is disabled (e.g., during Wails bindings generation or cross-compilation).

func Init(iconPath string) {}
func SetVisible(v bool) {}
func SetStatus(connected bool, rx, tx int64, workers int32) {}
func GtkMain() {}

var OnShow func()
var OnQuit func()

