//go:build linux

package tray

/*
#cgo pkg-config: ayatana-appindicator3-0.1
#include "linux_tray.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

func Init(iconPath string) {
	cPath := C.CString(iconPath)
	defer C.free(unsafe.Pointer(cPath))
	C.wdtt_tray_init(cPath)
}

func SetVisible(v bool) {
	vis := C.int(0)
	if v {
		vis = C.int(1)
	}
	C.wdtt_tray_set_visible(vis)
}

func SetStatus(connected bool, rx, tx int64, workers int32) {
	c := C.int(0)
	if connected {
		c = C.int(1)
	}
	C.wdtt_tray_set_status(c, C.longlong(rx), C.longlong(tx), C.int(workers))
}

func GtkMain() {
	C.wdtt_gtk_main()
}

//export onShowClicked
func onShowClicked() {
	if OnShow != nil {
		OnShow()
	}
}

//export onQuitClicked
func onQuitClicked() {
	if OnQuit != nil {
		OnQuit()
	}
}

var OnShow func()
var OnQuit func()
