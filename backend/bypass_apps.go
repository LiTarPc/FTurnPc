package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
// executable path (C:\\Program Files\\Steam\\steam.exe). Paths are reduced to
// their executable basename because the UI models bypasses by application.
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
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
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

// ApplyProcessBypassApps injects user-selected split-tunnel applications into
// a generated sing-box config. The returned JSON is suitable for sing-box
// check/run and preserves numbers without converting them through float64.
func ApplyProcessBypassApps(data []byte, apps []string) ([]byte, error) {
	apps = normalizeBypassApps(apps)
	if len(apps) == 0 {
		return data, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cfg map[string]interface{}
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse generated sing-box config for app bypass: %w", err)
	}
	if err := applyProcessBypassApps(cfg, apps); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sing-box app bypass config: %w", err)
	}
	return out, nil
}

func bypassProcessPathRegex(app string) string {
	// sing-box process_name matching is case-sensitive. Windows process paths
	// are not usefully case-sensitive, so match the basename using a
	// case-insensitive path regexp instead. The expression also matches a bare
	// process name when the platform reports only the basename.
	return `(?i)(?:^|[\\/])` + regexp.QuoteMeta(app) + `$`
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

	processRegexes := make([]interface{}, 0, len(apps))
	for _, app := range apps {
		processRegexes = append(processRegexes, bypassProcessPathRegex(app))
	}
	bypassRule := map[string]interface{}{
		"process_path_regex": processRegexes,
		"action":             "route",
		"outbound":           "direct",
	}

	// The user bypass rule must be evaluated before protocol=dns hijack.
	route["rules"] = append([]interface{}{bypassRule}, rules...)
	return nil
}
