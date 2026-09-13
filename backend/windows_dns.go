//go:build windows

package backend

import (
	"log"
	"time"
)

// CleanupNetworkLeftovers очищает зависшие правила NRPT и брандмауэра от предыдущих версий.
// Вызывается при старте приложения для очистки legacy-правил.
func CleanupNetworkLeftovers() {
	log.Printf("[SB] Очистка legacy DNS/firewall правил...")

	// Удаление устаревших NRPT-правил и брандмауэра от старой WireGuard версии
	psBatch := "$ErrorActionPreference = 'SilentlyContinue'; Get-DnsClientNrptRule | Where-Object { $_.DisplayName -eq 'FTurn_DNS_Rule' } | Remove-DnsClientNrptRule -Force; Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_UDP','FTurn_Block_DNS_Leak_TCP';"
	_ = runWithTimeout(10*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", psBatch)

	// Восстановление Smart Name Resolution в реестре (если был изменён старой версией)
	_ = run("reg", "delete", "HKLM\\Software\\Policies\\Microsoft\\Windows NT\\DNSClient",
		"/v", "DisableSmartNameResolution", "/f")

	_ = run("ipconfig", "/flushdns")
}
