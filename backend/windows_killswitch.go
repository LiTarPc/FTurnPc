//go:build windows

package backend

import (
	"crypto/md5"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

// BrowserInfo содержит информацию об обнаруженном браузере
type BrowserInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ExePath string `json:"exePath"`
	Enabled bool   `json:"enabled"`
}

var (
	ksMu          sync.Mutex
	ksActiveRules []string // Активные имена правил в брандмауэре
)

// cleanBrowserPath очищает путь из реестра (убирает кавычки, аргументы вроде "%1")
func cleanBrowserPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "\"") {
		parts := strings.Split(raw[1:], "\"")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}
	lower := strings.ToLower(raw)
	if idx := strings.Index(lower, ".exe"); idx != -1 {
		return strings.Trim(raw[:idx+4], "\" ")
	}
	return raw
}

// isIgnoredBrowser проверяет, не входит ли браузер в список исключённых
func isIgnoredBrowser(p string) bool {
	lower := strings.ToLower(p)
	if strings.Contains(lower, "yandex") || strings.Contains(lower, "browser.exe") || strings.Contains(lower, "iexplore.exe") {
		return true
	}
	return false
}

// getBrowserDisplayName формирует красивое имя браузера по его пути
func getBrowserDisplayName(exePath string) string {
	lower := strings.ToLower(exePath)
	switch {
	case strings.Contains(lower, "chrome.exe"):
		return "Google Chrome"
	case strings.Contains(lower, "msedge.exe"):
		return "Microsoft Edge"
	case strings.Contains(lower, "firefox.exe"):
		return "Mozilla Firefox"
	case strings.Contains(lower, "opera gx"):
		return "Opera GX"
	case strings.Contains(lower, "opera.exe"):
		return "Opera"
	case strings.Contains(lower, "brave.exe"):
		return "Brave Browser"
	case strings.Contains(lower, "vivaldi.exe"):
		return "Vivaldi"
	case strings.Contains(lower, "tor.exe"):
		return "Tor Browser"
	default:
		base := filepath.Base(exePath)
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext)
	}
}

// scanRegistryForBrowsers сканирует ветку реестра StartMenuInternet
func scanRegistryForBrowsers(root registry.Key) []string {
	k, err := registry.OpenKey(root, `SOFTWARE\Clients\StartMenuInternet`, registry.READ)
	if err != nil {
		return nil
	}
	defer k.Close()

	subkeys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var results []string
	for _, sk := range subkeys {
		if strings.Contains(strings.ToLower(sk), "yandex") {
			continue
		}
		cmdPath := fmt.Sprintf(`SOFTWARE\Clients\StartMenuInternet\%s\shell\open\command`, sk)
		cmdKey, err := registry.OpenKey(root, cmdPath, registry.READ)
		if err != nil {
			continue
		}
		val, _, err := cmdKey.GetStringValue("")
		cmdKey.Close()
		if err != nil {
			continue
		}

		clean := cleanBrowserPath(val)
		if clean != "" && !isIgnoredBrowser(clean) {
			if _, statErr := os.Stat(clean); statErr == nil {
				results = append(results, clean)
			}
		}
	}
	return results
}

// DetectInstalledBrowsers ищет установленные в системе браузеры (реестр, стандартные пути)
func DetectInstalledBrowsers() []BrowserInfo {
	found := make(map[string]bool)
	var list []BrowserInfo

	addPath := func(p string) {
		if p == "" || isIgnoredBrowser(p) {
			return
		}
		clean := filepath.Clean(p)
		lower := strings.ToLower(clean)
		if found[lower] {
			return
		}
		if _, err := os.Stat(clean); err == nil {
			found[lower] = true
			name := getBrowserDisplayName(clean)
			id := fmt.Sprintf("%x", md5.Sum([]byte(lower)))[:8]
			list = append(list, BrowserInfo{
				ID:      id,
				Name:    name,
				ExePath: clean,
				Enabled: true,
			})
		}
	}

	// 1. Сканируем реестр Windows (HKLM и HKCU)
	for _, p := range scanRegistryForBrowsers(registry.LOCAL_MACHINE) {
		addPath(p)
	}
	for _, p := range scanRegistryForBrowsers(registry.CURRENT_USER) {
		addPath(p)
	}

	// 2. Стандартные пути установки популярных браузеров
	progFiles := os.Getenv("ProgramFiles")
	progFilesX86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LocalAppData")

	standardLocations := []string{
		// Google Chrome
		filepath.Join(progFiles, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(progFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),

		// Microsoft Edge
		filepath.Join(progFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(progFiles, "Microsoft", "Edge", "Application", "msedge.exe"),

		// Mozilla Firefox
		filepath.Join(progFiles, "Mozilla Firefox", "firefox.exe"),
		filepath.Join(progFilesX86, "Mozilla Firefox", "firefox.exe"),

		// Brave Browser
		filepath.Join(progFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),

		// Opera
		filepath.Join(localAppData, "Programs", "Opera", "opera.exe"),
		filepath.Join(progFiles, "Opera", "opera.exe"),

		// Opera GX
		filepath.Join(localAppData, "Programs", "Opera GX", "opera.exe"),
		filepath.Join(progFiles, "Opera GX", "opera.exe"),

		// Vivaldi
		filepath.Join(localAppData, "Vivaldi", "Application", "vivaldi.exe"),
		filepath.Join(progFiles, "Vivaldi", "Application", "vivaldi.exe"),
	}

	for _, loc := range standardLocations {
		addPath(loc)
	}

	return list
}

// ApplyBrowserKillSwitch накладывает правила блокировки в брандмауэре Windows для указанных браузеров
func ApplyBrowserKillSwitch(paths []string) error {
	ksMu.Lock()
	defer ksMu.Unlock()

	var validPaths []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" && !isIgnoredBrowser(p) {
			if _, err := os.Stat(p); err == nil {
				validPaths = append(validPaths, p)
			}
		}
	}

	if len(validPaths) == 0 {
		return nil
	}

	log.Printf("[Kill-Switch] Активация блокировки браузеров (%d шт.)...", len(validPaths))

	// Формируем пакет команд для netsh через временный файл
	var batchContent strings.Builder
	var newRules []string

	for i, path := range validPaths {
		ruleName := fmt.Sprintf("FTurn_KS_%d", i)
		newRules = append(newRules, ruleName)

		// Сначала удаляем возможно зависшее правило с таким же именем, затем добавляем блокирующее
		batchContent.WriteString(fmt.Sprintf("advfirewall firewall delete rule name=\"%s\"\n", ruleName))
		batchContent.WriteString(fmt.Sprintf("advfirewall firewall add rule name=\"%s\" dir=out action=block program=\"%s\" enable=yes\n", ruleName, path))
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("ft_ks_apply_%d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, []byte(batchContent.String()), 0600); err != nil {
		return fmt.Errorf("write ks batch file: %w", err)
	}
	defer os.Remove(tmpFile)

	start := time.Now()
	err := runWithTimeout(5*time.Second, "netsh", "-f", tmpFile)
	if err != nil {
		log.Printf("[Kill-Switch] Ошибка netsh при наложении правил: %v", err)
		return err
	}

	ksActiveRules = newRules
	log.Printf("[Kill-Switch] Правила блокировки успешно применены за %v (%d правил)", time.Since(start), len(newRules))
	return nil
}

// RemoveBrowserKillSwitch снимает правила блокировки браузеров
func RemoveBrowserKillSwitch() error {
	ksMu.Lock()
	defer ksMu.Unlock()

	if len(ksActiveRules) == 0 {
		// Для подстраховки проверим через PowerShell, нет ли оставшихся правил
		_ = runWithTimeout(5*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Remove-NetFirewallRule -DisplayName 'FTurn_KS_*' -ErrorAction SilentlyContinue")
		return nil
	}

	log.Printf("[Kill-Switch] Снятие блокировки браузеров (%d правил)...", len(ksActiveRules))

	var batchContent strings.Builder
	for _, ruleName := range ksActiveRules {
		batchContent.WriteString(fmt.Sprintf("advfirewall firewall delete rule name=\"%s\"\n", ruleName))
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("ft_ks_remove_%d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, []byte(batchContent.String()), 0600); err != nil {
		ksActiveRules = nil
		return fmt.Errorf("write ks remove batch: %w", err)
	}
	defer os.Remove(tmpFile)

	_ = runWithTimeout(5*time.Second, "netsh", "-f", tmpFile)
	ksActiveRules = nil

	log.Printf("[Kill-Switch] Блокировка браузеров успешно снята.")
	return nil
}

// CleanupAllKillSwitchRules очищает любые правила FTurn_KS_* при старте или завершении приложения
func CleanupAllKillSwitchRules() {
	ksMu.Lock()
	defer ksMu.Unlock()

	ksActiveRules = nil
	_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='SilentlyContinue'; Remove-NetFirewallRule -DisplayName 'FTurn_KS_*'")
}
