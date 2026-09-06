//go:build !windows

package backend

type BrowserInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ExePath string `json:"exePath"`
	Enabled bool   `json:"enabled"`
}

func DetectInstalledBrowsers() []BrowserInfo {
	return nil
}

func ApplyBrowserKillSwitch(paths []string) error {
	return nil
}

func RemoveBrowserKillSwitch() error {
	return nil
}

func CleanupAllKillSwitchRules() {
}
