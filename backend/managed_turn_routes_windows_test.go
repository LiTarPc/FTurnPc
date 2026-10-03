//go:build windows

package backend

import (
	"os/exec"
	"testing"

	xwindows "golang.org/x/sys/windows"
)

func TestParseDefaultGatewayCandidates(t *testing.T) {
	output := `Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0         26.0.0.1   26.102.141.156   9257
          0.0.0.0          0.0.0.0   10.139.228.238   10.139.228.111     55
          0.0.0.0        128.0.0.0         On-link       10.13.13.17      2
Persistent Routes:
          0.0.0.0          0.0.0.0         26.0.0.1    9256`
	got := parseDefaultGatewayCandidates(output)
	if len(got) != 2 || got[0].gateway != "10.139.228.238" || got[0].ifaceIP != "10.139.228.111" || got[1].gateway != "26.0.0.1" {
		t.Fatalf("candidates = %+v, want Wi-Fi before Radmin", got)
	}
	if !isVirtualTurnAdapter("Radmin VPN") || !isVirtualTurnAdapter("WireGuard Tunnel") ||
		isVirtualTurnAdapter("Беспроводная сеть") {
		t.Fatal("physical/virtual adapter classification is incorrect")
	}
}

func TestPeerRouteAloneDoesNotStartSingbox(t *testing.T) {
	e := &FreeturnEngine{
		cmd:                &exec.Cmd{},
		ftReady:            true,
		protectedPeerIP:    "45.133.251.171",
		routePendingWarned: true,
	}
	e.startSingboxWhenReady()
	if e.sbStarting || e.sbApplied {
		t.Fatal("peer route was mistaken for a TURN route")
	}
}

func TestPhysicalTurnAdapterMetadata(t *testing.T) {
	if !isPhysicalTurnAdapterRow(xwindows.IF_TYPE_IEEE80211, 0b101) ||
		!isPhysicalTurnAdapterRow(xwindows.IF_TYPE_ETHERNET_CSMACD, 0b001) ||
		isPhysicalTurnAdapterRow(xwindows.IF_TYPE_ETHERNET_CSMACD, 0) ||
		isPhysicalTurnAdapterRow(53, 0b001) {
		t.Fatal("hardware Ethernet/Wi-Fi classification is incorrect")
	}
}
