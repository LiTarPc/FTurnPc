//go:build windows

package backend

import (
	"syscall"
	"unsafe"
)

var (
	user32DLL               = syscall.NewLazyDLL("user32.dll")
	findWindowW             = user32DLL.NewProc("FindWindowW")
	dwmapiDLL               = syscall.NewLazyDLL("dwmapi.dll")
	dwmSetWindowAttribute   = dwmapiDLL.NewProc("DwmSetWindowAttribute")
)

func getHwndByTitle(title string) uintptr {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	hwnd, _, _ := findWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

func (a *App) SetWindowTheme(dark bool) {
	hwnd := getHwndByTitle("FTurnPc")
	if hwnd == 0 {
		return
	}
	var value int32
	if dark {
		value = 1
	}
	// Try attribute 20 (Windows 10 20H1+ and Windows 11)
	_, _, _ = dwmSetWindowAttribute.Call(
		hwnd,
		20,
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	// Try attribute 19 (older Windows 10)
	_, _, _ = dwmSetWindowAttribute.Call(
		hwnd,
		19,
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
}
