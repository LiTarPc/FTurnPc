package backend

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestParseWGConfig_ValidSections(t *testing.T) {
	input := `[Interface]
Address = 10.0.0.2/32
DNS = 1.1.1.1, 8.8.8.8
MTU = 1380
PrivateKey = private
PostUp = should-be-stripped

[Peer]
PublicKey = public
Endpoint = 203.0.113.10:51820
AllowedIPs = 0.0.0.0/0, 10.0.0.0/8
PersistentKeepalive = 25
`
	addr, mtu, allowed, dns, wgConf := parseWGConfig(input)

	if addr != "10.0.0.2/32" {
		t.Fatalf("addr = %q", addr)
	}
	if mtu != "1380" {
		t.Fatalf("mtu = %q", mtu)
	}
	if !reflect.DeepEqual(allowed, []string{"0.0.0.0/0", "10.0.0.0/8"}) {
		t.Fatalf("allowedIPs = %#v", allowed)
	}
	if !reflect.DeepEqual(dns, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("dns = %#v", dns)
	}

	for _, removed := range []string{"Address =", "DNS =", "MTU =", "PostUp ="} {
		if strings.Contains(wgConf, removed) {
			t.Fatalf("wg setconf output still contains wg-quick-only field %q:\n%s", removed, wgConf)
		}
	}
	for _, kept := range []string{"[Interface]", "PrivateKey = private", "[Peer]", "PublicKey = public", "AllowedIPs = 0.0.0.0/0, 10.0.0.0/8"} {
		if !strings.Contains(wgConf, kept) {
			t.Fatalf("wg setconf output lost %q:\n%s", kept, wgConf)
		}
	}
}

func TestParseWGConfig_IgnoresFieldsOutsideCorrectSections(t *testing.T) {
	input := `Address = 192.0.2.1/32
AllowedIPs = 192.0.2.0/24

[Interface]
PrivateKey = private

[Peer]
PublicKey = public
`
	addr, _, allowed, _, _ := parseWGConfig(input)
	if addr != "" {
		t.Fatalf("Address outside [Interface] was accepted: %q", addr)
	}
	if len(allowed) != 0 {
		t.Fatalf("AllowedIPs outside [Peer] were accepted: %#v", allowed)
	}
}

func TestParseWGConfig_CaseInsensitiveSectionsAndKeys(t *testing.T) {
	input := `[interface]
aDdReSs = 10.0.0.5/32
mTu = 1300
dNs = 9.9.9.9
PrivateKey = private

[pEeR]
PublicKey = public
aLlOwEdIpS = 0.0.0.0/0
`
	addr, mtu, allowed, dns, _ := parseWGConfig(input)
	if addr != "10.0.0.5/32" || mtu != "1300" {
		t.Fatalf("addr/mtu = %q/%q", addr, mtu)
	}
	if !reflect.DeepEqual(allowed, []string{"0.0.0.0/0"}) {
		t.Fatalf("allowed = %#v", allowed)
	}
	if !reflect.DeepEqual(dns, []string{"9.9.9.9"}) {
		t.Fatalf("dns = %#v", dns)
	}
}

func TestParseWGConfig_AggregatesAllowedIPsAcrossPeers(t *testing.T) {
	input := `[Interface]
Address = 10.0.0.2/32
PrivateKey = private

[Peer]
PublicKey = one
AllowedIPs = 10.0.0.0/8

[Peer]
PublicKey = two
AllowedIPs = 172.16.0.0/12, 192.168.0.0/16
`
	_, _, allowed, _, _ := parseWGConfig(input)
	want := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	if !reflect.DeepEqual(allowed, want) {
		t.Fatalf("allowed = %#v, want %#v", allowed, want)
	}
}

func TestParseWGConfig_DefaultDNS(t *testing.T) {
	_, _, _, dns, _ := parseWGConfig(`[Interface]
Address = 10.0.0.2/32
PrivateKey = private
`)
	want := []string{"1.1.1.1", "1.0.0.1"}
	if !reflect.DeepEqual(dns, want) {
		t.Fatalf("dns = %#v, want %#v", dns, want)
	}
}

func TestParseWGConfig_EmptyInput(t *testing.T) {
	addr, mtu, allowed, dns, wgConf := parseWGConfig("")
	if addr != "" || mtu != "" || len(allowed) != 0 || wgConf != "" {
		t.Fatalf("unexpected parse result: addr=%q mtu=%q allowed=%#v wg=%q", addr, mtu, allowed, wgConf)
	}
	if !reflect.DeepEqual(dns, []string{"1.1.1.1", "1.0.0.1"}) {
		t.Fatalf("dns = %#v", dns)
	}
}

func TestGetVKExcludeCIDRs_AreValidIPv4Networks(t *testing.T) {
	cidrs := GetVKExcludeCIDRs()
	if len(cidrs) == 0 {
		t.Fatal("VK exclusion list is empty")
	}
	seen := map[string]bool{}
	for _, cidr := range cidrs {
		ip, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatalf("invalid VK CIDR %q: %v", cidr, err)
		}
		if ip.To4() == nil || network.IP.To4() == nil {
			t.Fatalf("VK CIDR is not IPv4: %q", cidr)
		}
		canonical := network.String()
		if canonical != cidr {
			t.Fatalf("VK CIDR %q is not canonical; use %q", cidr, canonical)
		}
		if seen[cidr] {
			t.Fatalf("duplicate VK CIDR %q", cidr)
		}
		seen[cidr] = true
	}
}
