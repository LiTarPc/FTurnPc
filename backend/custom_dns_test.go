package backend

import "testing"

func TestCustomDNSConfig(t *testing.T) {
	for _, tc := range []struct {
		input, typ, host, resolver string
	}{
		{"", "udp", "1.1.1.1", ""},
		{"94.140.14.14", "udp", "94.140.14.14", ""},
		{"tls://dns.adguard-dns.com", "tls", "dns.adguard-dns.com", "dns-local"},
		{"https://dns.adguard-dns.com/dns-query", "https", "dns.adguard-dns.com", "dns-local"},
	} {
		server, err := buildRemoteDNSServer(tc.input, "1.1.1.1", "proxy")
		if err != nil {
			t.Fatalf("%q: %v", tc.input, err)
		}
		if server["type"] != tc.typ || server["server"] != tc.host || server["detour"] != "proxy" || server["domain_resolver"] != valueOrNil(tc.resolver) {
			t.Errorf("%q: %#v", tc.input, server)
		}
	}
	for _, invalid := range []string{"dns.adguard-dns.com", "https://user:pass@dns.example/dns-query", "tls://dns.example:99999", "ftp://dns.example"} {
		if _, err := buildRemoteDNSServer(invalid, "1.1.1.1", "proxy"); err == nil {
			t.Errorf("%q accepted", invalid)
		}
	}
}

func valueOrNil(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func TestBuildSingboxConfigUsesCustomDNS(t *testing.T) {
	profile := &ProfileData{SB: sbRawString(t, "vless://"+sbTestUUID+"@origin.example:443?security=tls")}
	cfg := sbBuildJSON(t, profile, ConnectParams{DNSServer: "https://dns.adguard-dns.com/dns-query"})
	dns := sbRequireMap(t, cfg, "dns")
	server := sbFindByStringField(t, sbRequireArray(t, dns, "servers"), "tag", "dns-remote")
	sbRequireString(t, server, "type", "https")
	sbRequireString(t, server, "server", "dns.adguard-dns.com")
	sbRequireString(t, server, "path", "/dns-query")
	sbRequireString(t, server, "domain_resolver", "dns-local")
	sbRequireString(t, server, "detour", "proxy")
}
