//go:build windows

package backend

import (
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// wintunDLL is set by InitWintun called from main_windows.go
var wintunDLL []byte

var (
	activeDevice        *device.Device
	activeTun           tun.Device
	activeExcludeRoutes []string
	activeGatewayIP     string
	activeIfaceIndex    int
)

func InitWintun(dll []byte) { wintunDLL = dll }

// extractWintun writes the embedded wintun.dll next to the exe so the wintun
// package can load it via LoadLibrary.
func extractWintun() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if _, err := os.Stat(dst); err == nil {
		return nil // already extracted
	}
	return os.WriteFile(dst, wintunDLL, 0644)
}

func applyWGConfig(conf string, turnIPs []string, bypassRu bool, customMTU int) error {
	teardownWG()

	if err := extractWintun(); err != nil {
		return fmt.Errorf("extract wintun.dll: %w", err)
	}

	addr, mtuStr, allowedIPs, dnsServers, wgConf := parseWGConfig(conf)
	if addr == "" {
		return fmt.Errorf("Address not found in wg config")
	}

	mtu := 1300
	if customMTU >= 576 && customMTU <= 1500 {
		mtu = customMTU
	} else if mtuStr != "" {
		fmt.Sscanf(mtuStr, "%d", &mtu)
	}

	log.Printf("[WG] Creating Wintun interface %s with MTU %d...", wgIface, mtu)
	tunDev, err := tun.CreateTUN(wgIface, mtu)
	if err != nil {
		return fmt.Errorf("create TUN: %w", err)
	}
	activeTun = tunDev

	// Create userspace WireGuard device
	logger := &device.Logger{
		Verbosef: func(format string, args ...interface{}) {},
		Errorf:   func(format string, args ...interface{}) { log.Printf("[WG] "+format, args...) },
	}
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)
	activeDevice = dev

	if err := dev.IpcSetOperation(strings.NewReader(uapiConf(wgConf))); err != nil {
		return fmt.Errorf("IpcSet: %w", err)
	}

	if err := dev.Up(); err != nil {
		return fmt.Errorf("device up: %w", err)
	}

	// Set IP address on the interface
	if err := run("netsh", "interface", "ip", "set", "address",
		"name="+wgIface, "source=static", addr, "none"); err != nil {
		// addr may be CIDR — extract host part
		host, mask, _ := parseCIDR(addr)
		if host != "" {
			_ = run("netsh", "interface", "ip", "set", "address",
				"name="+wgIface, "source=static", host, mask)
		}
	}

	// Принудительная установка MTU на субинтерфейсе Windows TCP/IP для предотвращения фрагментации UDP пакетов
	log.Printf("[WG] Настройка системного MTU=%d на интерфейсе %s...", mtu, wgIface)
	if err := run("netsh", "interface", "ipv4", "set", "subinterface", wgIface, fmt.Sprintf("mtu=%d", mtu), "store=active"); err != nil {
		log.Printf("[WG] Предупреждение netsh MTU: %v", err)
	}

	// Принудительное назначение безопасных DNS-серверов на интерфейс wg-turn для устранения утечек DNS
	if len(dnsServers) > 0 {
		log.Printf("[WG] Настройка безопасных DNS (%s) на интерфейсе %s...", strings.Join(dnsServers, ", "), wgIface)
		if err := run("netsh", "interface", "ip", "set", "dns",
			"name="+wgIface, "source=static", dnsServers[0], "register=primary", "validate=no"); err != nil {
			log.Printf("[WG] Предупреждение netsh первичный DNS: %v", err)
		}
		if len(dnsServers) > 1 {
			_ = run("netsh", "interface", "ip", "add", "dns",
				"name="+wgIface, "address="+dnsServers[1], "index=2", "validate=no")
		}
	}

	// Exclude routes BEFORE adding tunnel routes
	gw := defaultGateway()
	log.Printf("[WG] Default gateway: %s", gw)
	if gw != "" {
		var excludes []string
		for _, ip := range turnIPs {
			excludes = append(excludes, ip+"/32")
		}
		excludes = append(excludes, vkExcludeCIDRs...)

		if bypassRu {
			ruCIDRs := loadGeoIPRuCIDRs()
			excludes = append(excludes, ruCIDRs...)
		}

		ifIndex, _ := getGatewayInterfaceIndex(gw)
		activeGatewayIP = gw
		activeIfaceIndex = ifIndex

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
		activeExcludeRoutes = excludes
	}

	// Add AllowedIPs routes via the WG interface.
	// Split 0.0.0.0/0 into 0.0.0.0/1 + 128.0.0.0/1 so they are more specific
	// than the existing default route and always win without needing metric tricks.
	var expandedIPs []string
	for _, cidr := range allowedIPs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "0.0.0.0/0" {
			expandedIPs = append(expandedIPs, "0.0.0.0/1", "128.0.0.0/1")
		} else {
			expandedIPs = append(expandedIPs, cidr)
		}
	}
	for _, cidr := range expandedIPs {
		if err := run("netsh", "interface", "ip", "add", "route", cidr, wgIface, "metric=1"); err != nil {
			log.Printf("[WG] add route %s err: %v", cidr, err)
		} else {
			log.Printf("[WG] route added: %s via %s", cidr, wgIface)
		}
	}

	log.Printf("[WG] Туннель %s поднят (userspace)", wgIface)
	return nil
}

func teardownWG() {
	if len(activeExcludeRoutes) > 0 {
		log.Printf("[WG] Deleting %d exclude routes...", len(activeExcludeRoutes))
		start := time.Now()

		gw := activeGatewayIP
		ifIndex := activeIfaceIndex

		tmpFile, err := os.CreateTemp("", "ft_routes_del_*.txt")
		if err == nil {
			defer os.Remove(tmpFile.Name())
			var content strings.Builder
			for _, cidr := range activeExcludeRoutes {
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
		activeExcludeRoutes = nil
		activeGatewayIP = ""
		activeIfaceIndex = 0
	}

	if activeDevice != nil {
		activeDevice.Close()
		activeDevice = nil
	}
	if activeTun != nil {
		activeTun.Close()
		activeTun = nil
	}
}

// uapiConf converts a wg-setconf-compatible config (with [Interface]/[Peer] sections)
// into the UAPI protocol format expected by device.IpcSetOperation.
//
// UAPI format: flat key=value, no section headers, hex keys, starts with "set=1\n",
// peers separated by a blank line.
func uapiConf(wgConf string) string {
	var sb strings.Builder
	inPeer := false
	for _, line := range strings.Split(wgConf, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "[Interface]" {
			inPeer = false
			continue
		}
		if trimmed == "[Peer]" {
			if inPeer {
				sb.WriteString("\n") // blank line separates peers
			}
			inPeer = true
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "privatekey":
			sb.WriteString("private_key=" + toHex(val) + "\n")
		case "listenport":
			sb.WriteString("listen_port=" + val + "\n")
		case "publickey":
			sb.WriteString("public_key=" + toHex(val) + "\n")
		case "presharedkey":
			sb.WriteString("preshared_key=" + toHex(val) + "\n")
		case "endpoint":
			sb.WriteString("endpoint=" + val + "\n")
		case "allowedips":
			for _, cidr := range strings.Split(val, ",") {
				if c := strings.TrimSpace(cidr); c != "" {
					sb.WriteString("allowed_ip=" + c + "\n")
				}
			}
		case "persistentkeepalive":
			sb.WriteString("persistent_keepalive_interval=" + val + "\n")
		}
	}
	sb.WriteString("\n") // final terminator
	return sb.String()
}

// toHex converts a base64-encoded WireGuard key to lowercase hex.
func toHex(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return b64 // already hex or garbage — return as-is
	}
	return hex.EncodeToString(raw)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func defaultGateway() string {
	cmd := exec.Command("cmd", "/c", "route print 0.0.0.0")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			return fields[2]
		}
	}
	return ""
}

// parseCIDR converts "10.0.0.2/24" → ("10.0.0.2", "255.255.255.0", nil).
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

type MIB_IFROW struct {
	wszName           [256]uint16
	dwIndex           uint32
	dwType            uint32
	dwMtu             uint32
	dwSpeed           uint32
	dwPhysAddrLen     uint32
	bPhysAddr         [8]byte
	dwAdminStatus     uint32
	dwOperStatus      uint32
	dwLastChange      uint32
	dwInOctets        uint32
	dwInUcastPkts     uint32
	dwInNUcastPkts    uint32
	dwInDiscards      uint32
	dwInErrors        uint32
	dwInUnknownProtos uint32
	dwOutOctets       uint32
	dwOutUcastPkts    uint32
	dwOutNUcastPkts   uint32
	dwOutDiscards     uint32
	dwOutErrors       uint32
	dwOutQLen         uint32
	dwDescrLen        uint32
	bDescr            [256]byte
}

var (
	iphlpapi        = syscall.NewLazyDLL("iphlpapi.dll")
	procGetIfEntry  = iphlpapi.NewProc("GetIfEntry")
)

func getInterfaceBytes(ifaceName string) (rx, tx int64, err error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return 0, 0, err
	}
	row := MIB_IFROW{
		dwIndex: uint32(iface.Index),
	}
	ret, _, _ := procGetIfEntry.Call(uintptr(unsafe.Pointer(&row)))
	if ret != 0 {
		return 0, 0, fmt.Errorf("GetIfEntry returned error: %d", ret)
	}
	return int64(row.dwInOctets), int64(row.dwOutOctets), nil
}

func getGatewayInterfaceIndex(gwStr string) (int, error) {
	gw := net.ParseIP(gwStr)
	if gw == nil {
		return 0, fmt.Errorf("invalid gateway IP")
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return 0, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if ok && !ipnet.IP.IsLoopback() {
				if ipnet.Contains(gw) {
					return iface.Index, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("interface for gateway not found")
}
