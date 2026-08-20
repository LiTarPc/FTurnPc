//go:build linux

package backend

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func CleanupNetworkLeftovers() {
	// Для Linux очистка (пока не требуется на старте)
}

var (
	activeRoutes    []string
	activeRoutesMu  sync.Mutex
	activeGatewayIP string
)

func applyWGConfig(conf string, turnIPs []string, bypassRu bool, customMTU int) error {
	teardownWG()

	addr, mtu, allowedIPs, _, wgConf := parseWGConfig(conf)
	if addr == "" {
		return fmt.Errorf("Address not found in wg config")
	}

	mtuVal := 1300
	if customMTU >= 576 && customMTU <= 1500 {
		mtuVal = customMTU
	} else if mtu != "" {
		fmt.Sscanf(mtu, "%d", &mtuVal)
	}

	// Check if sudo is available
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		return fmt.Errorf("sudo недоступен (нет прав без пароля): %w", err)
	}

	tmp, err := os.CreateTemp("", "wg-turn-*.conf")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(wgConf); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	_ = os.Chmod(tmpName, 0o644)

	if err := run("ip", "link", "add", wgIface, "type", "wireguard"); err != nil {
		return fmt.Errorf("ip link add: %w", err)
	}

	if err := run("wg", "setconf", wgIface, tmpName); err != nil {
		return fmt.Errorf("wg setconf: %w", err)
	}

	_ = run("ip", "addr", "flush", "dev", wgIface)
	if err := run("ip", "addr", "add", addr, "dev", wgIface); err != nil {
		return fmt.Errorf("ip addr add: %w", err)
	}
	if mtu != "" {
		_ = run("ip", "link", "set", wgIface, "mtu", mtu)
	}
	if err := run("ip", "link", "set", wgIface, "up"); err != nil {
		return fmt.Errorf("ip link set up: %w", err)
	}

	gw := defaultGateway()
	activeGatewayIP = gw
	log.Printf("[WG] Linux default gateway: %s", gw)

	var routes []string
	if gw != "" {
		var excludes []string
		for _, ip := range turnIPs {
			excludes = append(excludes, ip+"/32")
		}
		excludes = append(excludes, GetVKExcludeCIDRs()...)

		if bypassRu {
			ruCIDRs := loadGeoIPRuCIDRs()
			excludes = append(excludes, ruCIDRs...)
		}

		// Apply exclude routes via batch script under sudo for instant application
		if len(excludes) > 0 {
			start := time.Now()
			log.Printf("[WG] Adding %d exclude routes on Linux...", len(excludes))
			var batchCmds strings.Builder
			for _, cidr := range excludes {
				batchCmds.WriteString(fmt.Sprintf("route add %s via %s\n", cidr, gw))
				routes = append(routes, cidr)
			}
			if err := runBatchIPCommands(batchCmds.String()); err != nil {
				log.Printf("[WG] Batch route add warning: %v", err)
			} else {
				log.Printf("[WG] Added %d exclude routes in %v", len(excludes), time.Since(start))
			}
		}
	}

	for _, cidr := range allowedIPs {
		if run("ip", "route", "add", cidr, "dev", wgIface) == nil {
			routes = append(routes, "dev:"+cidr)
		}
	}

	activeRoutesMu.Lock()
	activeRoutes = routes
	activeRoutesMu.Unlock()
	return nil
}

func teardownWG() {
	activeRoutesMu.Lock()
	routes := activeRoutes
	activeRoutes = nil
	activeGatewayIP = ""
	activeRoutesMu.Unlock()

	if len(routes) > 0 {
		var batchCmds strings.Builder
		for _, entry := range routes {
			if strings.HasPrefix(entry, "dev:") {
				cidr := strings.TrimPrefix(entry, "dev:")
				batchCmds.WriteString(fmt.Sprintf("route del %s dev %s\n", cidr, wgIface))
			} else {
				batchCmds.WriteString(fmt.Sprintf("route del %s\n", entry))
			}
		}
		_ = runBatchIPCommands(batchCmds.String())
	}
	_ = run("ip", "link", "del", wgIface)
}

// AddBypassRoute добавляет маршрут-исключение "на лету" без полного перезапуска интерфейса.
func AddBypassRoute(ip string) error {
	gw := activeGatewayIP
	if gw == "" {
		gw = defaultGateway()
		if gw == "" {
			return fmt.Errorf("no default gateway found")
		}
		activeGatewayIP = gw
	}
	err := run("ip", "route", "add", ip+"/32", "via", gw)
	if err == nil {
		activeRoutesMu.Lock()
		activeRoutes = append(activeRoutes, ip+"/32")
		activeRoutesMu.Unlock()
	}
	return err
}


func runBatchIPCommands(script string) error {
	tmp, err := os.CreateTemp("", "ip-batch-*.txt")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	_ = os.Chmod(tmpName, 0o644)
	return run("ip", "-batch", tmpName)
}

func run(name string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{"-n", name}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func defaultGateway() string {
	cmd := exec.Command("ip", "route", "show", "default")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func IsInternetAvailable() bool {
	return defaultGateway() != ""
}

func HasNetworkChanged() bool {
	if activeGatewayIP == "" {
		return defaultGateway() == ""
	}
	gw := defaultGateway()
	if gw == "" {
		return true // Internet lost
	}
	if gw != activeGatewayIP {
		return true // Gateway IP changed
	}
	return false
}

func localDNSServers() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip == nil || ip.IsLoopback() {
			continue
		}
		result = append(result, fields[1])
	}
	return result
}

func getInterfaceBytes(ifaceName string) (rx, tx int64, err error) {
	rxBytes, err1 := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/statistics/rx_bytes", ifaceName))
	txBytes, err2 := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/statistics/tx_bytes", ifaceName))
	if err1 == nil {
		fmt.Sscanf(strings.TrimSpace(string(rxBytes)), "%d", &rx)
	}
	if err2 == nil {
		fmt.Sscanf(strings.TrimSpace(string(txBytes)), "%d", &tx)
	}
	return rx, tx, nil
}
