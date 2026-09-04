//go:build windows

package backend

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// applyDNSLeakProtection применяет комплексную защиту от утечек DNS (SMHNR, NRPT, брандмауэр Windows).
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

	// 4. Добавление NRPT-правила (Name Resolution Policy Table) для перенаправления запросов всех доменов (.) на DNS туннеля
	var quotedServers []string
	for _, s := range dnsServers {
		quotedServers = append(quotedServers, fmt.Sprintf("'%s'", strings.TrimSpace(s)))
	}
	psCmd := fmt.Sprintf("Add-DnsClientNrptRule -Namespace '.' -NameServers @(%s) -DisplayName 'FTurn_DNS_Rule' -ErrorAction SilentlyContinue",
		strings.Join(quotedServers, ","))
	_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)

	// 5. Блокировка DNS-запросов мимо туннеля через физический адаптер в брандмауэре Windows
	if ifIndex > 0 {
		if iface, err := net.InterfaceByIndex(ifIndex); err == nil && iface.Name != "" {
			_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_UDP' -ErrorAction SilentlyContinue")
			_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_TCP' -ErrorAction SilentlyContinue")

			psCmdUDP := fmt.Sprintf("New-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_UDP' -Direction Outbound -Action Block -Protocol UDP -RemotePort 53 -InterfaceAlias '%s' -ErrorAction SilentlyContinue", iface.Name)
			_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmdUDP)

			psCmdTCP := fmt.Sprintf("New-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_TCP' -Direction Outbound -Action Block -Protocol TCP -RemotePort 53 -InterfaceAlias '%s' -ErrorAction SilentlyContinue", iface.Name)
			_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmdTCP)
		}
	}

	// 6. Сброс системного DNS-кэша
	_ = run("ipconfig", "/flushdns")
}

// teardownDNSLeakProtection корректно очищает правила NRPT, брандмауэра и реестра.
func teardownDNSLeakProtection() {
	log.Printf("[WG-DNS] Очистка правил защиты от утечек DNS...")

	// 1. Удаление NRPT-правила
	_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-DnsClientNrptRule -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -eq 'FTurn_DNS_Rule' } | Remove-DnsClientNrptRule -Force -ErrorAction SilentlyContinue")

	// 2. Восстановление Smart Name Resolution в реестре
	_ = run("reg", "delete", "HKLM\\Software\\Policies\\Microsoft\\Windows NT\\DNSClient",
		"/v", "DisableSmartNameResolution", "/f")

	// 3. Удаление правил брандмауэра
	_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_UDP' -ErrorAction SilentlyContinue")
	_ = runWithTimeout(7*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Remove-NetFirewallRule -DisplayName 'FTurn_Block_DNS_Leak_TCP' -ErrorAction SilentlyContinue")

	// 4. Сброс DNS-кэша
	_ = run("ipconfig", "/flushdns")
}

// CleanupNetworkLeftovers очищает зависшие правила NRPT и брандмауэра при старте приложения.
func CleanupNetworkLeftovers() {
	teardownDNSLeakProtection()
}
