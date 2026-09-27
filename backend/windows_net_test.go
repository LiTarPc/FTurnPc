//go:build windows

package backend

import "testing"

func TestDefaultRouteSkipsDisconnectedAdapter(t *testing.T) {
	routes := []MIB_IPFORWARDROW{
		{DwForwardIfIndex: 4, DwForwardNextHop: 0x0100000a, DwForwardMetric1: 5},
		{DwForwardIfIndex: 8, DwForwardNextHop: 0x0100a8c0, DwForwardMetric1: 20},
	}
	linkUp := func(index int) bool { return index == 8 }
	selected := selectDefaultRoute(routes, linkUp)
	if !selected.valid || selected.ifIndex != 8 || selected.gateway != "192.168.0.1" {
		t.Fatalf("selected disconnected adapter instead of live backup: %+v", selected)
	}
	if selected := selectDefaultRoute(routes, func(int) bool { return false }); selected.valid {
		t.Fatalf("route remained available after both links dropped: %+v", selected)
	}
}

func TestNetworkChangeDetectsLossAndRestoration(t *testing.T) {
	online := defaultRouteSnapshot{gateway: "192.168.0.1", ifIndex: 8, valid: true}
	offline := defaultRouteSnapshot{gateway: "", ifIndex: 0, valid: false}
	if networkChanged(online, online) || !networkChanged(online, offline) || !networkChanged(offline, online) {
		t.Fatal("network transitions were not classified correctly")
	}
}
