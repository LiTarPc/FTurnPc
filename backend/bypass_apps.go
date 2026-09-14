package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	bypassAppsFileName = "bypass-apps.json"
	maxBypassApps      = 128
	maxBypassAppLen    = 260
)

func bypassAppsPath() string {
	return filepath.Join(configDir(), bypassAppsFileName)
}

// normalizeBypassApp accepts either a process name (steam.exe) or a pasted
// executable path (C:\\Program Files\\Steam\\steam.exe). sing-box process_name
// rules match the executable basename, so paths are deliberately reduced to
// their final component.
func normalizeBypassApp(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, `"'`)
	if value == "" || strings.ContainsRune(value, '\x00') || len(value) > maxBypassAppLen {
		return "", false
	}

	// path.Base is platform-independent when input is normalized to '/'. This
	// also makes Windows paths parse correctly in Linux/macOS unit tests.
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Base(value)
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || value == "/" {
		return "", false
	}
	return value, true
}

func normalizeBypassApps(apps []string) []string {
	result := make([]string, 0, min(len(apps), maxBypassApps))
	seen := make(map[string]struct{}, len(apps))
	for _, raw := range apps {
		name, ok := normalizeBypassApp(raw)
		if !ok {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
		if len(result) >= maxBypassApps {
			break
		}
	}
	return result
}

func loadBypassApps() []string {
	data, err := os.ReadFile(bypassAppsPath())
	if err != nil {
		return nil
	}
	var apps []string
	if err := json.Unmarshal(data, &apps); err != nil {
		return nil
	}
	return normalizeBypassApps(apps)
}

func saveBypassApps(apps []string) error {
	apps = normalizeBypassApps(apps)
	data, err := json.MarshalIndent(apps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bypass apps: %w", err)
	}
	p := bypassAppsPath()
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("save bypass apps: %w", err)
	}
	return os.Chmod(p, 0o600)
}

// GetBypassApps and SetBypassApps are exported through Wails. The setting is
// global rather than profile-scoped: an application selected for split tunnel
// stays direct regardless of the currently selected FreeTurn profile.
func (a *App) GetBypassApps() []string {
	return loadBypassApps()
}

func (a *App) SetBypassApps(apps []string) error {
	return saveBypassApps(apps)
}

// applyProcessBypassApps inserts a high-priority process rule before DNS
// hijacking. This is important: a bypassed application must use its normal
// network path, including its own DNS traffic, instead of being caught by the
// global VPN DNS hijack rule first.
func applyProcessBypassApps(cfg map[string]interface{}, apps []string) error {
	apps = normalizeBypassApps(apps)
	if len(apps) == 0 {
		return nil
	}

	route, ok := cfg["route"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("sing-box config route is missing or invalid")
	}
	rules, ok := route["rules"].([]interface{})
	if !ok {
		return fmt.Errorf("sing-box config route.rules is missing or invalid")
	}

	processNames := make([]interface{}, 0, len(apps))
	for _, app := range apps {
		processNames = append(processNames, app)
	}
	bypassRule := map[string]interface{}{
		"process_name": processNames,
		"action":       "route",
		"outbound":     "direct",
	}

	// The user bypass rule must be evaluated before protocol=dns hijack.
	route["rules"] = append([]interface{}{bypassRule}, rules...)
	return nil
}
