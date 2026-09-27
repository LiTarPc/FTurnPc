//go:build windows

package backend

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	xwindows "golang.org/x/sys/windows"
)

var procGetBestRoute = iphlpapi.NewProc("GetBestRoute")

func uiManagesTurnRoutes() bool { return true }

type gatewayCandidate struct {
	gateway string
	ifaceIP string
	metric  uint64
}

func parseDefaultGatewayCandidates(output string) []gatewayCandidate {
	var candidates []gatewayCandidate
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		// Active rows have five columns. Persistent rows have only four.
		if len(fields) != 5 || fields[0] != "0.0.0.0" || fields[1] != "0.0.0.0" {
			continue
		}
		gw, iface := net.ParseIP(fields[2]), net.ParseIP(fields[3])
		metric, err := strconv.ParseUint(fields[4], 10, 32)
		if gw == nil || gw.To4() == nil || iface == nil || iface.To4() == nil || err != nil {
			continue
		}
		candidates = append(candidates, gatewayCandidate{gateway: gw.String(), ifaceIP: iface.String(), metric: metric})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].metric < candidates[j].metric })
	return candidates
}

func physicalTurnGateway() (managedTurnRoute, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "route", "print", "-4", "0.0.0.0") //nolint:gosec
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return managedTurnRoute{}, "", fmt.Errorf("route print: %w: %s", err, out)
	}
	for _, candidate := range parseDefaultGatewayCandidates(string(out)) {
		for _, row := range getIPv4RouteRows() {
			if row.DwForwardDest != 0 || row.DwForwardMask != 0 || row.DwForwardIfIndex == 0 ||
				dwordIPv4(row.DwForwardNextHop).String() != candidate.gateway {
				continue
			}
			iface, err := net.InterfaceByIndex(int(row.DwForwardIfIndex))
			if err != nil || iface.Flags&net.FlagUp == 0 || !isPhysicalTurnAdapter(iface.Index) || isVirtualTurnAdapter(iface.Name) ||
				!interfaceHasIPv4(iface, candidate.ifaceIP) {
				continue
			}
			return managedTurnRoute{gateway: candidate.gateway, ifIndex: iface.Index}, iface.Name, nil
		}
	}
	return managedTurnRoute{}, "", fmt.Errorf("не найден активный физический IPv4-шлюз Ethernet/Wi-Fi")
}

func isPhysicalTurnAdapter(ifIndex int) bool {
	row := xwindows.MibIfRow2{InterfaceIndex: uint32(ifIndex)}
	if err := xwindows.GetIfEntry2Ex(xwindows.MibIfEntryNormal, &row); err != nil {
		return false
	}
	return isPhysicalTurnAdapterRow(row.Type, row.InterfaceAndOperStatusFlags)
}

func isPhysicalTurnAdapterRow(ifType uint32, flags uint8) bool {
	// MIB_IF_ROW2 bit 0 is HardwareInterface. Virtual VPN adapters may still
	// report Ethernet type 6, so both checks are needed.
	return (ifType == xwindows.IF_TYPE_ETHERNET_CSMACD || ifType == xwindows.IF_TYPE_IEEE80211) && flags&1 != 0
}

func interfaceHasIPv4(iface *net.Interface, rawIP string) bool {
	want := net.ParseIP(rawIP)
	addrs, err := iface.Addrs()
	if err != nil || want == nil {
		return false
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.Equal(want) {
			return true
		}
	}
	return false
}

func isVirtualTurnAdapter(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range []string{"radmin", "wireguard", "wintun", "vpn", "virtualbox", "hyper-v", "loopback", "tap", "tun", "wg-turn", "fturn"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func addManagedTurnRoute(rawIP string) (*managedTurnRoute, error) {
	ip := net.ParseIP(rawIP)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, fmt.Errorf("неверный TURN IPv4 адрес %q", rawIP)
	}
	route, ifaceName, err := physicalTurnGateway()
	if err != nil {
		return nil, err
	}
	if hasExactTurnRoute(ip, route) {
		if bestTurnRouteMatches(ip, route) {
			return nil, nil // Pre-existing route: do not take ownership.
		}
		return nil, fmt.Errorf("маршрут %s/32 через %s есть, но Windows выбирает другой интерфейс; отключите Radmin VPN", ip, ifaceName)
	}
	if err := changeTurnRoute("add", ip.String(), route); err != nil {
		return nil, err
	}
	if !bestTurnRouteMatches(ip, route) {
		if cleanupErr := changeTurnRoute("delete", ip.String(), route); cleanupErr != nil {
			return &route, fmt.Errorf("маршрут %s/32 проиграл другому интерфейсу, очистка не удалась: %w", ip, cleanupErr)
		}
		return nil, fmt.Errorf("Windows не выбрала %s для %s/32 через %s; отключите Radmin VPN и повторите подключение", ifaceName, ip, route.gateway)
	}
	return &route, nil
}

func deleteManagedTurnRoute(rawIP string, route managedTurnRoute) error {
	ip := net.ParseIP(rawIP)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("неверный TURN IP %q", rawIP)
	}
	if !hasExactTurnRoute(ip, route) {
		return nil
	}
	return changeTurnRoute("delete", ip.String(), route)
}

func changeTurnRoute(action, ip string, route managedTurnRoute) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := []string{"interface", "ipv4", action, "route", "prefix=" + ip + "/32",
		"interface=" + strconv.Itoa(route.ifIndex), "nexthop=" + route.gateway, "store=active"}
	if action == "add" {
		args = append(args, "metric=1")
	}
	cmd := exec.CommandContext(ctx, "netsh", args...) //nolint:gosec
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %s %s/32 via %s (interface %d): %w: %s", action, ip, route.gateway, route.ifIndex, err, out)
	}
	return nil
}

func hasExactTurnRoute(ip net.IP, route managedTurnRoute) bool {
	for _, row := range getIPv4RouteRows() {
		if dwordIPv4(row.DwForwardDest).Equal(ip) &&
			dwordIPv4(row.DwForwardMask).Equal(net.IPv4(255, 255, 255, 255)) &&
			dwordIPv4(row.DwForwardNextHop).String() == route.gateway &&
			int(row.DwForwardIfIndex) == route.ifIndex {
			return true
		}
	}
	return false
}

func bestTurnRouteMatches(ip net.IP, route managedTurnRoute) bool {
	var best MIB_IPFORWARDROW
	dest := binary.LittleEndian.Uint32(ip.To4())
	ret, _, _ := procGetBestRoute.Call(uintptr(dest), 0, uintptr(unsafe.Pointer(&best)))
	return ret == 0 && int(best.DwForwardIfIndex) == route.ifIndex &&
		dwordIPv4(best.DwForwardNextHop).String() == route.gateway
}

func verifyManagedTurnRoutes(ips []string) error {
	route, _, err := physicalTurnGateway()
	if err != nil {
		return err
	}
	for _, rawIP := range ips {
		ip := net.ParseIP(rawIP)
		if ip == nil || ip.To4() == nil || !bestTurnRouteMatches(ip, route) {
			return fmt.Errorf("TURN %s больше не идёт через физический интерфейс %d (%s)", rawIP, route.ifIndex, route.gateway)
		}
	}
	return nil
}
