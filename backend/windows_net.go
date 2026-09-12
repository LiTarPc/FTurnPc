//go:build windows

package backend

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// MIB_IFROW содержит низкоуровневые метрики сетевого интерфейса Windows.
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
	iphlpapi              = syscall.NewLazyDLL("iphlpapi.dll")
	procGetIfEntry        = iphlpapi.NewProc("GetIfEntry")
	procGetIpForwardTable = iphlpapi.NewProc("GetIpForwardTable")
)

// MIB_IPFORWARDROW описывает одну запись маршрута в IPv4.
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

// GetExistingRoutesFast получает все существующие IPv4-маршруты за миллисекунды через WinAPI.
// Возвращает мапу, где ключи — префиксы назначения в формате CIDR ("10.0.0.0/8").
func GetExistingRoutesFast() map[string]bool {
	existing := make(map[string]bool)
	var size uint32
	procGetIpForwardTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return existing
	}

	buf := make([]byte, size)
	ret, _, _ := procGetIpForwardTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
	if ret != 0 {
		return existing
	}

	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	for i := uint32(0); i < numEntries; i++ {
		row := (*MIB_IPFORWARDROW)(unsafe.Pointer(&buf[4+i*uint32(unsafe.Sizeof(MIB_IPFORWARDROW{}))]))
		
		dest := row.DwForwardDest
		mask := row.DwForwardMask
		
		ipStr := fmt.Sprintf("%d.%d.%d.%d", dest&0xff, (dest>>8)&0xff, (dest>>16)&0xff, (dest>>24)&0xff)
		ip := net.ParseIP(ipStr)
		maskIP := net.IPv4(byte(mask&0xff), byte((mask>>8)&0xff), byte((mask>>16)&0xff), byte((mask>>24)&0xff))
		
		if ip != nil {
			prefixSize, _ := net.IPMask(maskIP.To4()).Size()
			cidr := fmt.Sprintf("%s/%d", ip.String(), prefixSize)
			existing[cidr] = true
		}
	}
	return existing
}

// getInterfaceBytes считывает переданные и принятые байты через WinAPI GetIfEntry.
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

// defaultGateway определяет текущий активный шлюз по умолчанию в системе.
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

// getGatewayInterfaceIndex находит индекс интерфейса, через который доступен шлюз.
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

// IsInternetAvailable проверяет наличие маршрута по умолчанию к сети интернет.
func IsInternetAvailable() bool {
	return defaultGateway() != ""
}

// HasNetworkChanged определяет, изменился ли сетевой шлюз, интерфейс или пропал ли интернет.
func HasNetworkChanged() bool {
	activeRoutesMu.Lock()
	savedGw := activeGatewayIP
	savedIfIndex := activeIfaceIndex
	activeRoutesMu.Unlock()

	if savedGw == "" {
		return defaultGateway() == ""
	}
	gw := defaultGateway()
	if gw == "" {
		return true // Интернет пропал
	}
	ifIndex, err := getGatewayInterfaceIndex(gw)
	if err != nil || ifIndex != savedIfIndex {
		return true // Сменился физический интерфейс
	}
	if gw != savedGw {
		return true // Сменился IP-адрес шлюза
	}
	return false
}
