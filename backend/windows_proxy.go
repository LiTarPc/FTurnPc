//go:build windows

package backend

import (
	"log"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	INTERNET_OPTION_SETTINGS_CHANGED = 39
	INTERNET_OPTION_REFRESH          = 37
)

// SetSystemProxy enables or disables Windows System Proxy via HKCU registry and WinINet API
func SetSystemProxy(enable bool, proxyAddr string, bypassRu bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		log.Printf("[Proxy] Error opening registry key: %v", err)
		return err
	}
	defer k.Close()

	if enable {
		if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
			log.Printf("[Proxy] Error setting ProxyEnable: %v", err)
		}
		if err := k.SetStringValue("ProxyServer", proxyAddr); err != nil {
			log.Printf("[Proxy] Error setting ProxyServer: %v", err)
		}

		override := "<local>"
		if bypassRu {
			override = "<local>;*.ru;*.gov.ru;*.vk.com;*.yandex.ru;*.ya.ru;*.vk.ru"
		}
		if err := k.SetStringValue("ProxyOverride", override); err != nil {
			log.Printf("[Proxy] Error setting ProxyOverride: %v", err)
		}
		log.Printf("[Proxy] System proxy ENABLED -> %s (override: %s)", proxyAddr, override)
	} else {
		if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
			log.Printf("[Proxy] Error disabling ProxyEnable: %v", err)
		}
		log.Printf("[Proxy] System proxy DISABLED")
	}

	// Notify WinINet system settings changed so browsers refresh immediately
	wininet := syscall.NewLazyDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")

	internetSetOption.Call(0, uintptr(INTERNET_OPTION_SETTINGS_CHANGED), 0, 0)
	internetSetOption.Call(0, uintptr(INTERNET_OPTION_REFRESH), 0, 0)

	return nil
}
