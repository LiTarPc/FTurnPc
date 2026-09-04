//go:build windows

package backend

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	activeRoutesMu      sync.Mutex
	activeExcludeRoutes []string
	activeGatewayIP     string
	activeIfaceIndex    int
)

// applyExcludeRoutes подготавливает и применяет список исключений через пакетный файл netsh.
func applyExcludeRoutes(turnIPs []string, bypassRu bool) {
	gw := defaultGateway()
	log.Printf("[WG] Default gateway: %s", gw)
	if gw == "" {
		return
	}

	var excludes []string
	for _, ip := range turnIPs {
		parsed := net.ParseIP(ip)
		if parsed != nil && parsed.IsLoopback() {
			continue
		}
		excludes = append(excludes, ip+"/32")
	}
	excludes = append(excludes, GetVKExcludeCIDRs()...)

	if bypassRu {
		ruCIDRs := loadGeoIPRuCIDRs()
		excludes = append(excludes, ruCIDRs...)
	}

	ifIndex, _ := getGatewayInterfaceIndex(gw)

	activeRoutesMu.Lock()
	activeGatewayIP = gw
	activeIfaceIndex = ifIndex
	activeExcludeRoutes = excludes
	activeRoutesMu.Unlock()

	log.Printf("[WG] Adding %d exclude routes via netsh batch...", len(excludes))
	start := time.Now()

	tmpFile, err := os.CreateTemp("", "ft_routes_add_*.txt")
	if err == nil {
		defer os.Remove(tmpFile.Name())
		var content strings.Builder
		for _, cidr := range excludes {
			if ifIndex > 0 {
				content.WriteString(fmt.Sprintf("interface ipv4 add route prefix=%s interface=%d nexthop=%s metric=5 store=active\n", cidr, ifIndex, gw))
			} else {
				content.WriteString(fmt.Sprintf("interface ipv4 add route prefix=%s nexthop=%s metric=5 store=active\n", cidr, gw))
			}
		}
		_ = os.WriteFile(tmpFile.Name(), []byte(content.String()), 0644)
		_ = tmpFile.Close()

		if err := run("netsh", "-f", tmpFile.Name()); err != nil {
			log.Printf("[WG] netsh add routes err: %v", err)
		}
	} else {
		log.Printf("[WG] Failed to create temp file for routes: %v", err)
	}

	log.Printf("[WG] Added all exclude routes via netsh in %v", time.Since(start))
}

// deleteExcludeRoutes удаляет все ранее добавленные маршруты-исключения.
func deleteExcludeRoutes() {
	activeRoutesMu.Lock()
	routes := activeExcludeRoutes
	gw := activeGatewayIP
	ifIndex := activeIfaceIndex
	activeExcludeRoutes = nil
	activeGatewayIP = ""
	activeIfaceIndex = 0
	activeRoutesMu.Unlock()

	if len(routes) == 0 {
		return
	}

	log.Printf("[WG] Deleting %d exclude routes...", len(routes))
	start := time.Now()

	tmpFile, err := os.CreateTemp("", "ft_routes_del_*.txt")
	if err == nil {
		defer os.Remove(tmpFile.Name())
		var content strings.Builder
		for _, cidr := range routes {
			if ifIndex > 0 {
				content.WriteString(fmt.Sprintf("interface ipv4 delete route prefix=%s interface=%d nexthop=%s store=active\n", cidr, ifIndex, gw))
			} else {
				content.WriteString(fmt.Sprintf("interface ipv4 delete route prefix=%s nexthop=%s store=active\n", cidr, gw))
			}
		}
		_ = os.WriteFile(tmpFile.Name(), []byte(content.String()), 0644)
		_ = tmpFile.Close()

		if err := run("netsh", "-f", tmpFile.Name()); err != nil {
			log.Printf("[WG] netsh delete routes err: %v", err)
		}
	} else {
		log.Printf("[WG] Failed to create temp file for route deletion: %v", err)
	}

	log.Printf("[WG] Deleted all exclude routes via netsh in %v", time.Since(start))
}

// AddBypassRoute добавляет маршрут-исключение "на лету" без полного перезапуска интерфейса.
func AddBypassRoute(ip string) error {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed != nil && parsed.IsLoopback() {
		return nil // Для localhost/127.0.0.1 маршрутизация через шлюз не требуется
	}

	activeRoutesMu.Lock()
	gw := activeGatewayIP
	ifIndex := activeIfaceIndex
	if gw == "" {
		gw = defaultGateway()
		if gw != "" {
			activeGatewayIP = gw
			ifIndex, _ = getGatewayInterfaceIndex(gw)
			activeIfaceIndex = ifIndex
		}
	}
	activeRoutesMu.Unlock()

	if gw == "" {
		return fmt.Errorf("no default gateway found")
	}

	cidr := ip + "/32"
	var err error
	if ifIndex > 0 {
		err = run("netsh", "interface", "ipv4", "add", "route", "prefix="+cidr, fmt.Sprintf("interface=%d", ifIndex), "nexthop="+gw, "metric=5", "store=active")
	} else {
		err = run("netsh", "interface", "ipv4", "add", "route", "prefix="+cidr, "nexthop="+gw, "metric=5", "store=active")
	}
	if err == nil {
		activeRoutesMu.Lock()
		activeExcludeRoutes = append(activeExcludeRoutes, cidr)
		activeRoutesMu.Unlock()
	}
	return err
}

// parseCIDR преобразует запись подсети "10.0.0.2/24" в ("10.0.0.2", "255.255.255.0", nil).
func parseCIDR(cidr string) (ip, mask string, err error) {
	parts := strings.SplitN(cidr, "/", 2)
	if len(parts) != 2 {
		return cidr, "255.255.255.255", nil
	}
	ip = parts[0]
	var prefix int
	if _, e := fmt.Sscanf(parts[1], "%d", &prefix); e != nil || prefix < 0 || prefix > 32 {
		return "", "", fmt.Errorf("invalid prefix %q", parts[1])
	}
	var m uint32
	if prefix > 0 {
		m = ^uint32(0) << (32 - prefix)
	}
	mask = fmt.Sprintf("%d.%d.%d.%d", m>>24, (m>>16)&0xff, (m>>8)&0xff, m&0xff)
	return ip, mask, nil
}
