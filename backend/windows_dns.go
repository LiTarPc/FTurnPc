//go:build windows

package backend

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// applyDNSLeakProtection применяет комплексную защиту от утечек DNS (SMHNR, NRPT, брандмауэр Windows).
// Все PowerShell операции сгруппированы в единый пакет с таймаутом 5 секунд для мгновенного выполнения.
func applyDNSLeakProtection(dnsServers []string, ifIndex int) {
	if len(dnsServers) == 0 {
		return
	}

	log.Printf("[WG-DNS] Применение комплексной защиты от утечек DNS (%s)...", strings.Join(dnsServers, ", "))

	// 1. Установка безопасного DNS на интерфейс wg-turn
	if err := run("netsh", "interface", "ip", "set", "dns",
		"name="+wgIface, "source=static", dnsServers[0], "register=primary", "validate=no"); err != nil {
		log.Printf("[WG-DNS] Предупреждение netsh первичный DNS: %v", err)
	}
	if len(dnsServers) > 1 {
		_ = run("netsh", "interface", "ip", "add", "dns",
			"name="+wgIface, "address="+dnsServers[1], "index=2", "validate=no")
	}

	// 2. Установка максимального приоритета (низшей метрики) для wg-turn
	_ = run("netsh", "interface", "ipv4", "set", "interface", wgIface, "metric=1")

	// 3. Отключение Windows Smart Multi-Homed Name Resolution (SMHNR) в реестре
	_ = run("reg", "add", "HKLM\\Software\\Policies\\Microsoft\\Windows NT\\DNSClient",
		"/v", "DisableSmartNameResolution", "/t", "REG_DWORD", "/d", "1", "/f")

	// 4. Добавление NRPT-правила (Name Resolution Policy Table) для перенаправления всех DNS-запросов (.) в туннель
	// и удаление устаревших правил блокировки порта 53 на физическом адаптере, чтобы не блокировать VK Auth
	var quotedServers []string
	for _, s := range dnsServers {
		quotedServers = append(quotedServers, fmt.Sprintf("'%s'", strings.TrimSpace(s)))
	}

	var psBatch strings.Builder
	psBatch.WriteString("$ErrorActionPreference = 'SilentlyContinue'; ")
	psBatch.WriteString("Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_UDP','FTurn_Block_DNS_Leak_TCP'; ")
	psBatch.WriteString(fmt.Sprintf("Add-DnsClientNrptRule -Namespace '.' -NameServers @(%s) -DisplayName 'FTurn_DNS_Rule'; ", strings.Join(quotedServers, ",")))

	_ = runWithTimeout(15*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", psBatch.String())

	// 6. Сброс системного DNS-кэша
	_ = run("ipconfig", "/flushdns")
}

// teardownDNSLeakProtection корректно очищает правила NRPT, брандмауэра и реестра.
func teardownDNSLeakProtection() {
	log.Printf("[WG-DNS] Очистка правил защиты от утечек DNS...")

	// 1. Очистка NRPT и правил брандмауэра в одном вызове PowerShell
	psBatch := "$ErrorActionPreference = 'SilentlyContinue'; Get-DnsClientNrptRule | Where-Object { $_.DisplayName -eq 'FTurn_DNS_Rule' } | Remove-DnsClientNrptRule -Force; Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_UDP','FTurn_Block_DNS_Leak_TCP';"
	_ = runWithTimeout(10*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", psBatch)

	// 2. Восстановление Smart Name Resolution в реестре
	_ = run("reg", "delete", "HKLM\\Software\\Policies\\Microsoft\\Windows NT\\DNSClient",
		"/v", "DisableSmartNameResolution", "/f")

	// 3. Сброс DNS-кэша
	_ = run("ipconfig", "/flushdns")
}

// CleanupNetworkLeftovers очищает зависшие правила NRPT и брандмауэра при старте приложения.
func CleanupNetworkLeftovers() {
	teardownDNSLeakProtection()
	CleanupAllKillSwitchRules()
}
