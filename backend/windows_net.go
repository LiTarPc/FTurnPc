//go:build windows

package backend

import (
	"fmt"
	"net"
	"sync"
	"syscall"
	"unsafe"

	xwindows "golang.org/x/sys/windows"
)

type MIB_IPFORWARDROW struct {
	DwForwardDest      uint32
	DwForwardMask      uint32
	DwForwardPolicy    uint32
	DwForwardNextHop   uint32
	DwForwardIfIndex   uint32
	DwForwardType      uint32
	DwForwardProto     uint32
	DwForwardAge       uint32
	DwForwardNextHopAS uint32
	DwForwardMetric1   uint32
	DwForwardMetric2   uint32
	DwForwardMetric3   uint32
	DwForwardMetric4   uint32
	DwForwardMetric5   uint32
}

var (
	iphlpapi              = syscall.NewLazyDLL("iphlpapi.dll")
	procGetIpForwardTable = iphlpapi.NewProc("GetIpForwardTable")
)

func getIPv4RouteRows() []MIB_IPFORWARDROW {
	var size uint32
	_, _, _ = procGetIpForwardTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size < 4 {
		return nil
	}
	buf := make([]byte, size)
	ret, _, _ := procGetIpForwardTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
	if ret != 0 || len(buf) < 4 {
		return nil
	}

	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := int(unsafe.Sizeof(MIB_IPFORWARDROW{}))
	maxRows := (len(buf) - 4) / rowSize
	if int(count) > maxRows {
		count = uint32(maxRows)
	}
	rows := make([]MIB_IPFORWARDROW, 0, count)
	for i := uint32(0); i < count; i++ {
		offset := 4 + int(i)*rowSize
		row := *(*MIB_IPFORWARDROW)(unsafe.Pointer(&buf[offset]))
		rows = append(rows, row)
	}
	return rows
}

func dwordIPv4(v uint32) net.IP {
	return net.IPv4(byte(v&0xff), byte((v>>8)&0xff), byte((v>>16)&0xff), byte((v>>24)&0xff))
}

func GetExistingRoutesFast() map[string]bool {
	existing := make(map[string]bool)
	for _, row := range getIPv4RouteRows() {
		ip := dwordIPv4(row.DwForwardDest)
		maskIP := dwordIPv4(row.DwForwardMask)
		ones, bits := net.IPMask(maskIP.To4()).Size()
		if bits != 32 {
			continue
		}
		existing[fmt.Sprintf("%s/%d", ip.String(), ones)] = true
	}
	return existing
}

// getInterfaceBytes reads the actual 64-bit byte counters of fturn-tun.
// The old GetIfEntry/MIB_IFROW API exposed only 32-bit dwInOctets/dwOutOctets,
// which wrapped around under sustained traffic and produced bogus totals/speeds.
func getInterfaceBytes(ifaceName string) (rx, tx int64, err error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return 0, 0, err
	}

	row := xwindows.MibIfRow2{InterfaceIndex: uint32(iface.Index)}
	if err := xwindows.GetIfEntry2Ex(xwindows.MibIfEntryNormal, &row); err != nil {
		return 0, 0, fmt.Errorf("GetIfEntry2Ex(%s): %w", ifaceName, err)
	}

	return int64(row.InOctets), int64(row.OutOctets), nil
}

type defaultRouteSnapshot struct {
	gateway string
	ifIndex int
	metric  uint32
	valid   bool
}

// defaultRouteFast avoids spawning `route print` every monitor tick. The
// selected row is the lowest-metric IPv4 default route from IP Helper API.
func defaultRouteFast() defaultRouteSnapshot {
	var best defaultRouteSnapshot
	for _, row := range getIPv4RouteRows() {
		if row.DwForwardDest != 0 || row.DwForwardMask != 0 || row.DwForwardIfIndex == 0 {
			continue
		}
		if !best.valid || row.DwForwardMetric1 < best.metric {
			gw := dwordIPv4(row.DwForwardNextHop).String()
			best = defaultRouteSnapshot{
				gateway: gw,
				ifIndex: int(row.DwForwardIfIndex),
				metric:  row.DwForwardMetric1,
				valid:   true,
			}
		}
	}
	return best
}

func defaultGateway() string {
	route := defaultRouteFast()
	if !route.valid {
		return ""
	}
	return route.gateway
}

func getGatewayInterfaceIndex(gwStr string) (int, error) {
	gw := net.ParseIP(gwStr)
	if gw == nil {
		return 0, fmt.Errorf("invalid gateway IP")
	}
	// Prefer the route table itself; it is faster and handles adapters whose
	// gateway is not inside a locally configured subnet.
	for _, row := range getIPv4RouteRows() {
		if row.DwForwardDest == 0 && row.DwForwardMask == 0 && dwordIPv4(row.DwForwardNextHop).Equal(gw) {
			return int(row.DwForwardIfIndex), nil
		}
	}
	return 0, fmt.Errorf("interface for gateway not found")
}

func IsInternetAvailable() bool {
	return defaultRouteFast().valid
}

var (
	netMonMu       sync.Mutex
	lastGwIP       string
	lastIfIndex    int
	lastRouteValid bool
	netMonInit     bool
)

func HasNetworkChanged() bool {
	current := defaultRouteFast()

	netMonMu.Lock()
	defer netMonMu.Unlock()
	if !netMonInit {
		lastGwIP = current.gateway
		lastIfIndex = current.ifIndex
		lastRouteValid = current.valid
		netMonInit = true
		return false
	}
	changed := current.valid != lastRouteValid || current.gateway != lastGwIP || current.ifIndex != lastIfIndex
	lastGwIP = current.gateway
	lastIfIndex = current.ifIndex
	lastRouteValid = current.valid
	return changed
}
