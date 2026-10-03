package backend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const sbTestUUID = "00000000-0000-0000-0000-000000000001"

func TestSBDetectConfigType_Classification(t *testing.T) {
	tests := []struct {
		name   string
		sb     json.RawMessage
		wgConf string
		want   ConfigType
	}{
		{name: "nil", want: ConfigTypeUnknown},
		{name: "null", sb: json.RawMessage(`null`), want: ConfigTypeUnknown},
		{name: "empty string", sb: sbRawString(t, ""), want: ConfigTypeUnknown},
		{name: "json object", sb: json.RawMessage(`{"outbounds":[]}`), want: ConfigTypeSingboxJSON},
		{name: "json object encoded as string", sb: sbRawString(t, `{"outbounds":[]}`), want: ConfigTypeSingboxJSON},
		{name: "raw vless URI", sb: json.RawMessage("vless://" + sbTestUUID + "@example.com:443"), want: ConfigTypeURI},
		{name: "quoted vless URI", sb: sbRawString(t, "vless://"+sbTestUUID+"@example.com:443"), want: ConfigTypeURI},
		{name: "trojan", sb: sbRawString(t, "trojan://secret@example.com:443"), want: ConfigTypeURI},
		{name: "shadowsocks", sb: sbRawString(t, "ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388"), want: ConfigTypeURI},
		{name: "hysteria2", sb: sbRawString(t, "hysteria2://secret@example.com:443"), want: ConfigTypeURI},
		{name: "hy2 alias", sb: sbRawString(t, "hy2://secret@example.com:443"), want: ConfigTypeURI},
		{name: "tuic", sb: sbRawString(t, "tuic://"+sbTestUUID+":secret@example.com:443"), want: ConfigTypeURI},
		{name: "URI scheme is case insensitive", sb: sbRawString(t, "VLESS://"+sbTestUUID+"@example.com:443"), want: ConfigTypeURI},
		{name: "WG fallback", wgConf: "[Interface]\nAddress = 10.0.0.2/32", want: ConfigTypeWG},
		{
			name:   "SB has precedence over WG fallback",
			sb:     sbRawString(t, "vless://"+sbTestUUID+"@example.com:443"),
			wgConf: "[Interface]\nAddress = 10.0.0.2/32",
			want:   ConfigTypeURI,
		},
		{name: "unknown", sb: sbRawString(t, "not-a-config"), want: ConfigTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectConfigType(tt.sb, tt.wgConf); got != tt.want {
				t.Fatalf("DetectConfigType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSBParseProxyURI_ValidMatrix(t *testing.T) {
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:secret-password"))

	tests := []struct {
		name     string
		uri      string
		typeName string
		check    func(t *testing.T, out map[string]interface{})
	}{
		{
			name:     "vless tls ws",
			uri:      "vless://" + sbTestUUID + "@origin.example:443?security=tls&sni=sni.example&type=ws&host=cdn.example&path=%2Fws",
			typeName: "vless",
			check: func(t *testing.T, out map[string]interface{}) {
				sbRequireString(t, out, "uuid", sbTestUUID)
				tls := sbRequireMap(t, out, "tls")
				sbRequireBool(t, tls, "enabled", true)
				sbRequireString(t, tls, "server_name", "sni.example")
				transport := sbRequireMap(t, out, "transport")
				sbRequireString(t, transport, "type", "ws")
				sbRequireString(t, transport, "path", "/ws")
				headers := sbRequireMap(t, transport, "headers")
				sbRequireString(t, headers, "Host", "cdn.example")
			},
		},
		{
			name:     "trojan tls",
			uri:      "trojan://secret@example.com:443?security=tls&sni=trojan.example",
			typeName: "trojan",
			check: func(t *testing.T, out map[string]interface{}) {
				sbRequireString(t, out, "password", "secret")
				tls := sbRequireMap(t, out, "tls")
				sbRequireBool(t, tls, "enabled", true)
				sbRequireString(t, tls, "server_name", "trojan.example")
			},
		},
		{
			name:     "shadowsocks SIP002 base64",
			uri:      "ss://" + ssUser + "@origin.example:8388",
			typeName: "shadowsocks",
			check: func(t *testing.T, out map[string]interface{}) {
				sbRequireString(t, out, "method", "aes-128-gcm")
				sbRequireString(t, out, "password", "secret-password")
			},
		},
		{
			name:     "hysteria2",
			uri:      "hysteria2://secret@origin.example:443?sni=hy.example",
			typeName: "hysteria2",
			check: func(t *testing.T, out map[string]interface{}) {
				sbRequireString(t, out, "password", "secret")
				tls := sbRequireMap(t, out, "tls")
				sbRequireBool(t, tls, "enabled", true)
				sbRequireString(t, tls, "server_name", "hy.example")
			},
		},
		{
			name:     "hy2 alias",
			uri:      "hy2://secret@origin.example:443?sni=hy.example",
			typeName: "hysteria2",
			check: func(t *testing.T, out map[string]interface{}) {
				sbRequireString(t, out, "password", "secret")
			},
		},
		{
			name:     "tuic",
			uri:      "tuic://" + sbTestUUID + ":secret@origin.example:443?sni=tuic.example&congestion_control=bbr",
			typeName: "tuic",
			check: func(t *testing.T, out map[string]interface{}) {
				sbRequireString(t, out, "uuid", sbTestUUID)
				sbRequireString(t, out, "password", "secret")
				sbRequireString(t, out, "congestion_control", "bbr")
				tls := sbRequireMap(t, out, "tls")
				sbRequireBool(t, tls, "enabled", true)
				sbRequireString(t, tls, "server_name", "tuic.example")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := parseProxyURI(tt.uri)
			if err != nil {
				t.Fatalf("parseProxyURI(%q): %v", tt.uri, err)
			}
			sbRequireString(t, out, "type", tt.typeName)
			sbRequireString(t, out, "tag", "proxy")
			sbRequireString(t, out, "server", "127.0.0.1")
			sbRequireIntInterface(t, out, "server_port", 9000)
			tt.check(t, out)
		})
	}
}

func TestSBParseProxyURI_PreservesOriginalHostAsDefaultSNI(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{
			name: "vless tls",
			uri:  "vless://" + sbTestUUID + "@origin.example:443?security=tls",
		},
		{
			name: "trojan tls",
			uri:  "trojan://secret@origin.example:443?security=tls",
		},
		{
			name: "hysteria2 required TLS",
			uri:  "hysteria2://secret@origin.example:443",
		},
		{
			name: "tuic required TLS",
			uri:  "tuic://" + sbTestUUID + ":secret@origin.example:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := parseProxyURI(tt.uri)
			if err != nil {
				t.Fatalf("parseProxyURI(%q): %v", tt.uri, err)
			}
			tls := sbRequireMap(t, out, "tls")
			sbRequireBool(t, tls, "enabled", true)
			sbRequireString(t, tls, "server_name", "origin.example")
		})
	}
}

func TestSBParseProxyURI_RejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{name: "unsupported scheme", uri: "socks5://user:pass@example.com:1080"},
		{name: "vless missing UUID", uri: "vless://@example.com:443?security=tls"},
		{name: "vless missing host", uri: "vless://" + sbTestUUID + "@:443?security=tls"},
		{name: "vless malformed port", uri: "vless://" + sbTestUUID + "@example.com:notaport?security=tls"},
		{name: "trojan missing password", uri: "trojan://@example.com:443?security=tls"},
		{name: "shadowsocks malformed userinfo", uri: "ss://definitely-not-method-password@example.com:8388"},
		{name: "hysteria2 missing password", uri: "hysteria2://@example.com:443"},
		{name: "tuic missing password", uri: "tuic://" + sbTestUUID + "@example.com:443"},
		{name: "tuic missing UUID", uri: "tuic://:secret@example.com:443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseProxyURI(tt.uri); err == nil {
				t.Fatalf("parseProxyURI(%q) succeeded; malformed links must be rejected before config generation", tt.uri)
			}
		})
	}
}

func TestSBBuildWireGuard_UsesEndpointSchemaAndFreeTurnHop(t *testing.T) {
	priv := sbWGKey(0x11)
	pub := sbWGKey(0x22)
	psk := sbWGKey(0x33)
	profile := &ProfileData{WGConfig: fmt.Sprintf(`[Interface]
Address = 10.0.0.2/32
PrivateKey = %s
MTU = 1350
DNS = 1.1.1.1

[Peer]
PublicKey = %s
PresharedKey = %s
Endpoint = original.example:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 20
`, priv, pub, psk)}

	cfg := sbBuildJSON(t, profile, ConnectParams{MTU: 1350})
	endpoint := sbFindByStringField(t, sbRequireArray(t, cfg, "endpoints"), "type", "wireguard")

	if _, exists := endpoint["local_address"]; exists {
		t.Fatal("WireGuard endpoint contains deprecated outbound field local_address; endpoint schema requires address")
	}
	addresses := sbRequireStringArray(t, endpoint, "address")
	if !reflect.DeepEqual(addresses, []string{"10.0.0.2/32"}) {
		t.Fatalf("endpoint.address = %#v, want [10.0.0.2/32]", addresses)
	}
	sbRequireString(t, endpoint, "private_key", priv)
	sbRequireJSONInt(t, endpoint, "mtu", 1350)

	peers := sbRequireArray(t, endpoint, "peers")
	if len(peers) != 1 {
		t.Fatalf("len(peers) = %d, want 1", len(peers))
	}
	peer := sbRequireObjectValue(t, peers[0], "peers[0]")
	sbRequireString(t, peer, "address", "127.0.0.1")
	sbRequireJSONInt(t, peer, "port", 9000)
	sbRequireString(t, peer, "public_key", pub)
	sbRequireString(t, peer, "pre_shared_key", psk)
	allowed := sbRequireStringArray(t, peer, "allowed_ips")
	if !reflect.DeepEqual(allowed, []string{"0.0.0.0/0"}) {
		t.Fatalf("peer.allowed_ips = %#v, want [0.0.0.0/0]", allowed)
	}
	sbRequireJSONInt(t, peer, "persistent_keepalive_interval", 25)

	sbValidateGeneratedReferences(t, cfg)
}

func TestSBBuildWireGuard_InvalidLegacyMTUFallsBackTo1300(t *testing.T) {
	profile := &ProfileData{WGConfig: fmt.Sprintf(`[Interface]
Address = 10.0.0.2/32
PrivateKey = %s
MTU = 9000

[Peer]
PublicKey = %s
AllowedIPs = 0.0.0.0/0
`, sbWGKey(0x11), sbWGKey(0x22))}

	cfg := sbBuildJSON(t, profile, ConnectParams{})
	endpoint := sbFindByStringField(t, sbRequireArray(t, cfg, "endpoints"), "type", "wireguard")
	sbRequireJSONInt(t, endpoint, "mtu", 1300)
}

// Until multi-peer legacy conversion is implemented, silently selecting one
// peer is unsafe. Rejecting unsupported multi-peer input is the safer contract.
func TestSBBuildWireGuard_RejectsUnsupportedMultiplePeers(t *testing.T) {
	profile := &ProfileData{WGConfig: fmt.Sprintf(`[Interface]
Address = 10.0.0.2/32
PrivateKey = %s

[Peer]
PublicKey = %s
AllowedIPs = 0.0.0.0/0

[Peer]
PublicKey = %s
AllowedIPs = 10.0.0.0/8
`, sbWGKey(0x11), sbWGKey(0x22), sbWGKey(0x33))}

	if _, err := BuildSingboxConfig(profile, ConnectParams{}); err == nil {
		t.Fatal("multi-peer legacy WG config was silently accepted; support all peers or reject it explicitly")
	}
}

func TestSBBuildJSON_AssignsProxyTagToUntaggedUserOutbound(t *testing.T) {
	profile := &ProfileData{SB: json.RawMessage(`{
		"outbounds": [{
			"type": "vless",
			"server": "127.0.0.1",
			"server_port": 9000,
			"uuid": "00000000-0000-0000-0000-000000000001"
		}]
	}`)}

	cfg := sbBuildJSON(t, profile, ConnectParams{})
	proxy := sbFindByStringField(t, sbRequireArray(t, cfg, "outbounds"), "type", "vless")
	sbRequireString(t, proxy, "tag", "proxy")
	route := sbRequireMap(t, cfg, "route")
	sbRequireString(t, route, "final", "proxy")
	sbValidateGeneratedReferences(t, cfg)
}

func TestSBBuildJSON_RejectsReservedDirectTagCollision(t *testing.T) {
	profile := &ProfileData{SB: json.RawMessage(`{
		"outbounds": [{
			"type": "vless",
			"tag": "direct",
			"server": "127.0.0.1",
			"server_port": 9000,
			"uuid": "00000000-0000-0000-0000-000000000001"
		}]
	}`)}

	_, err := BuildSingboxConfig(profile, ConnectParams{})
	if err == nil {
		t.Fatal(`user proxy with reserved tag "direct" must be rejected`)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "direct") {
		t.Fatalf("error = %q, want an explicit reserved direct-tag error", err)
	}
}

func TestSBGeneratedConfig_TUNAndDNSInvariants(t *testing.T) {
	profile := &ProfileData{SB: sbRawString(t, "vless://"+sbTestUUID+"@origin.example:443?security=tls&sni=origin.example")}
	cfg := sbBuildJSON(t, profile, ConnectParams{})

	tun := sbFindByStringField(t, sbRequireArray(t, cfg, "inbounds"), "type", "tun")
	sbRequireString(t, tun, "interface_name", singTunName)
	sbRequireBool(t, tun, "auto_route", true)
	sbRequireBool(t, tun, "strict_route", true)
	sbRequireString(t, tun, "dns_mode", "hijack")

	route := sbRequireMap(t, cfg, "route")
	sbRequireBool(t, route, "auto_detect_interface", true)

	sbValidateGeneratedReferences(t, cfg)
}

func sbRawString(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal(%q): %v", s, err)
	}
	return b
}

func sbBuildJSON(t *testing.T, profile *ProfileData, params ConnectParams) map[string]interface{} {
	t.Helper()
	b, err := BuildSingboxConfig(profile, params)
	if err != nil {
		t.Fatalf("BuildSingboxConfig: %v", err)
	}
	return sbDecodeJSON(t, b)
}

func sbDecodeJSON(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var cfg map[string]interface{}
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v\n%s", err, b)
	}
	if dec.More() {
		t.Fatal("generated config contains trailing JSON values")
	}
	return cfg
}

func sbRequireMap(t *testing.T, m map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing object field %q", key)
	}
	return sbRequireObjectValue(t, v, key)
}

func sbRequireObjectValue(t *testing.T, v interface{}, where string) map[string]interface{} {
	t.Helper()
	out, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("%s has type %T, want object", where, v)
	}
	return out
}

func sbRequireArray(t *testing.T, m map[string]interface{}, key string) []interface{} {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing array field %q", key)
	}
	out, ok := v.([]interface{})
	if !ok {
		t.Fatalf("field %q has type %T, want array", key, v)
	}
	return out
}

func sbRequireString(t *testing.T, m map[string]interface{}, key, want string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing string field %q", key)
	}
	got, ok := v.(string)
	if !ok {
		t.Fatalf("field %q has type %T, want string %q", key, v, want)
	}
	if got != want {
		t.Fatalf("field %q = %q, want %q", key, got, want)
	}
}

func sbRequireBool(t *testing.T, m map[string]interface{}, key string, want bool) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing bool field %q", key)
	}
	got, ok := v.(bool)
	if !ok {
		t.Fatalf("field %q has type %T, want bool", key, v)
	}
	if got != want {
		t.Fatalf("field %q = %v, want %v", key, got, want)
	}
}

func sbRequireJSONInt(t *testing.T, m map[string]interface{}, key string, want int64) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing integer field %q", key)
	}
	n, ok := v.(json.Number)
	if !ok {
		t.Fatalf("field %q has type %T, want JSON number", key, v)
	}
	got, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		t.Fatalf("field %q = %q, want exact integer: %v", key, n.String(), err)
	}
	if got != want {
		t.Fatalf("field %q = %d, want %d", key, got, want)
	}
}

func sbRequireIntInterface(t *testing.T, m map[string]interface{}, key string, want int) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing integer field %q", key)
	}
	got, ok := v.(int)
	if !ok {
		t.Fatalf("field %q has type %T, want int", key, v)
	}
	if got != want {
		t.Fatalf("field %q = %d, want %d", key, got, want)
	}
}

func sbRequireStringArray(t *testing.T, m map[string]interface{}, key string) []string {
	t.Helper()
	raw := sbRequireArray(t, m, key)
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("field %q[%d] has type %T, want string", key, i, v)
		}
		out[i] = s
	}
	return out
}

func sbFindByStringField(t *testing.T, list []interface{}, key, want string) map[string]interface{} {
	t.Helper()
	var found map[string]interface{}
	for i, raw := range list {
		m := sbRequireObjectValue(t, raw, fmt.Sprintf("item[%d]", i))
		v, exists := m[key]
		if !exists {
			continue
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("item[%d].%s has type %T, want string", i, key, v)
		}
		if s == want {
			if found != nil {
				t.Fatalf("multiple objects with %s=%q", key, want)
			}
			found = m
		}
	}
	if found == nil {
		t.Fatalf("object with %s=%q not found", key, want)
	}
	return found
}

func sbWGKey(fill byte) string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = fill
	}
	return base64.StdEncoding.EncodeToString(b)
}

func sbStringOrList(t *testing.T, v interface{}, where string) []string {
	t.Helper()
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return []string{x}
	case []interface{}:
		out := make([]string, len(x))
		for i, item := range x {
			s, ok := item.(string)
			if !ok {
				t.Fatalf("%s[%d] has type %T, want string", where, i, item)
			}
			out[i] = s
		}
		return out
	default:
		t.Fatalf("%s has type %T, want string or string array", where, v)
		return nil
	}
}

func sbValidateGeneratedReferences(t *testing.T, cfg map[string]interface{}) {
	t.Helper()

	routeTags := map[string]string{}
	for _, section := range []string{"outbounds", "endpoints"} {
		raw, exists := cfg[section]
		if !exists {
			continue
		}
		list, ok := raw.([]interface{})
		if !ok {
			t.Fatalf("%s has type %T, want array", section, raw)
		}
		for i, item := range list {
			m := sbRequireObjectValue(t, item, fmt.Sprintf("%s[%d]", section, i))
			tag, _ := m["tag"].(string)
			if tag == "" {
				continue
			}
			if prev, duplicate := routeTags[tag]; duplicate {
				t.Fatalf("duplicate route tag %q in %s[%d]; already declared by %s", tag, section, i, prev)
			}
			routeTags[tag] = fmt.Sprintf("%s[%d]", section, i)
		}
	}

	if where, ok := routeTags["direct"]; ok {
		parts := strings.Split(where, "[")
		section := parts[0]
		var list []interface{}
		if raw, exists := cfg[section]; exists {
			list, _ = raw.([]interface{})
		}
		for _, item := range list {
			m, _ := item.(map[string]interface{})
			if tag, _ := m["tag"].(string); tag == "direct" {
				if typ, _ := m["type"].(string); typ != "direct" {
					t.Fatalf("reserved route tag direct is owned by type %q, want type direct", typ)
				}
			}
		}
	}

	requireRouteTag := func(where, tag string) {
		t.Helper()
		if tag == "" {
			t.Fatalf("%s is empty", where)
		}
		if _, ok := routeTags[tag]; !ok {
			t.Fatalf("%s references missing route tag %q; declared=%v", where, tag, routeTags)
		}
	}

	route := sbRequireMap(t, cfg, "route")
	final, ok := route["final"].(string)
	if !ok {
		t.Fatalf("route.final has type %T, want string", route["final"])
	}
	requireRouteTag("route.final", final)

	if rulesRaw, exists := route["rules"]; exists {
		rules, ok := rulesRaw.([]interface{})
		if !ok {
			t.Fatalf("route.rules has type %T, want array", rulesRaw)
		}
		for i, raw := range rules {
			rule := sbRequireObjectValue(t, raw, fmt.Sprintf("route.rules[%d]", i))
			if outbound, exists := rule["outbound"]; exists {
				tag, ok := outbound.(string)
				if !ok {
					t.Fatalf("route.rules[%d].outbound has type %T, want string", i, outbound)
				}
				requireRouteTag(fmt.Sprintf("route.rules[%d].outbound", i), tag)
			}
		}
	}

	ruleSetTags := map[string]bool{}
	if raw, exists := route["rule_set"]; exists {
		list, ok := raw.([]interface{})
		if !ok {
			t.Fatalf("route.rule_set has type %T, want array", raw)
		}
		for i, item := range list {
			rs := sbRequireObjectValue(t, item, fmt.Sprintf("route.rule_set[%d]", i))
			for _, tag := range sbStringOrList(t, rs["tag"], fmt.Sprintf("route.rule_set[%d].tag", i)) {
				if tag == "" {
					t.Fatalf("route.rule_set[%d] has empty tag", i)
				}
				if ruleSetTags[tag] {
					t.Fatalf("duplicate rule-set tag %q", tag)
				}
				ruleSetTags[tag] = true
			}
		}
	}

	validateRuleSets := func(owner string, rules []interface{}) {
		t.Helper()
		for i, raw := range rules {
			rule := sbRequireObjectValue(t, raw, fmt.Sprintf("%s[%d]", owner, i))
			if refs, exists := rule["rule_set"]; exists {
				for _, tag := range sbStringOrList(t, refs, fmt.Sprintf("%s[%d].rule_set", owner, i)) {
					if !ruleSetTags[tag] {
						t.Fatalf("%s[%d] references missing rule-set tag %q", owner, i, tag)
					}
				}
			}
		}
	}
	if raw, exists := route["rules"]; exists {
		validateRuleSets("route.rules", raw.([]interface{}))
	}

	dns := sbRequireMap(t, cfg, "dns")
	dnsTags := map[string]bool{}
	servers := sbRequireArray(t, dns, "servers")
	for i, raw := range servers {
		server := sbRequireObjectValue(t, raw, fmt.Sprintf("dns.servers[%d]", i))
		tag, ok := server["tag"].(string)
		if !ok || tag == "" {
			t.Fatalf("dns.servers[%d].tag = %#v, want non-empty string", i, server["tag"])
		}
		if dnsTags[tag] {
			t.Fatalf("duplicate DNS server tag %q", tag)
		}
		dnsTags[tag] = true
		if detour, exists := server["detour"]; exists {
			tag, ok := detour.(string)
			if !ok {
				t.Fatalf("dns.servers[%d].detour has type %T, want string", i, detour)
			}
			requireRouteTag(fmt.Sprintf("dns.servers[%d].detour", i), tag)
		}
	}

	if final, exists := dns["final"]; exists {
		tag, ok := final.(string)
		if !ok {
			t.Fatalf("dns.final has type %T, want string", final)
		}
		if !dnsTags[tag] {
			t.Fatalf("dns.final references missing DNS server %q", tag)
		}
	}

	if raw, exists := dns["rules"]; exists {
		rules, ok := raw.([]interface{})
		if !ok {
			t.Fatalf("dns.rules has type %T, want array", raw)
		}
		validateRuleSets("dns.rules", rules)
		for i, item := range rules {
			rule := sbRequireObjectValue(t, item, fmt.Sprintf("dns.rules[%d]", i))
			if serverRef, exists := rule["server"]; exists {
				tag, ok := serverRef.(string)
				if !ok {
					t.Fatalf("dns.rules[%d].server has type %T, want string", i, serverRef)
				}
				if !dnsTags[tag] {
					t.Fatalf("dns.rules[%d].server references missing DNS server %q", i, tag)
				}
			}
		}
	}

	if resolver, exists := route["default_domain_resolver"]; exists {
		m := sbRequireObjectValue(t, resolver, "route.default_domain_resolver")
		server, ok := m["server"].(string)
		if !ok {
			t.Fatalf("route.default_domain_resolver.server has type %T, want string", m["server"])
		}
		if !dnsTags[server] {
			t.Fatalf("route.default_domain_resolver.server references missing DNS server %q", server)
		}
	}
}

func TestSBBuildBypassRU_NeverReferencesMissingRuleSet(t *testing.T) {
	profile := &ProfileData{SB: sbRawString(t, "vless://"+sbTestUUID+"@origin.example:443?security=tls")}
	cfg := sbBuildJSON(t, profile, ConnectParams{BypassRu: true})
	sbValidateGeneratedReferences(t, cfg)
}

func TestSBBuildJSON_NormalizesKnownProxyToFreeTurnAndPreservesSNI(t *testing.T) {
	profile := &ProfileData{SB: json.RawMessage(`{
		"outbounds": [{
			"type": "vless",
			"tag": "proxy",
			"server": "origin.example",
			"server_port": 443,
			"uuid": "00000000-0000-0000-0000-000000000001",
			"tls": {"enabled": true}
		}]
	}`)}
	cfg := sbBuildJSON(t, profile, ConnectParams{})
	proxy := sbFindByStringField(t, sbRequireArray(t, cfg, "outbounds"), "tag", "proxy")
	sbRequireString(t, proxy, "server", freeTurnHost)
	sbRequireJSONInt(t, proxy, "server_port", freeTurnPort)
	tls := sbRequireMap(t, proxy, "tls")
	sbRequireString(t, tls, "server_name", "origin.example")
}

func TestSBRouteBypassesEveryFreeTurnExecutableName(t *testing.T) {
	profile := &ProfileData{SB: sbRawString(t, "vless://"+sbTestUUID+"@origin.example:443?security=tls")}
	cfg := sbBuildJSON(t, profile, ConnectParams{})
	route := sbRequireMap(t, cfg, "route")
	rules := sbRequireArray(t, route, "rules")
	want := map[string]bool{}
	for _, n := range freeturnBypassProcessNames() {
		want[n] = true
	}
	for _, raw := range rules {
		rule := sbRequireObjectValue(t, raw, "route rule")
		rawNames, ok := rule["process_name"].([]interface{})
		if !ok {
			continue
		}
		for _, rawName := range rawNames {
			name, ok := rawName.(string)
			if !ok {
				t.Fatalf("process_name contains %T, want string", rawName)
			}
			delete(want, name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("route does not bypass FreeTurn executable names: %v", want)
	}
}

func TestSBBuildJSON_MigratesLegacyWireGuardOutboundToEndpoint(t *testing.T) {
	priv := sbWGKey(0x11)
	pub := sbWGKey(0x22)
	profile := &ProfileData{SB: json.RawMessage(fmt.Sprintf(`{
		"outbounds": [{
			"type": "wireguard",
			"tag": "proxy",
			"server": "198.51.100.10",
			"server_port": 51820,
			"local_address": ["10.0.0.2/32"],
			"private_key": %q,
			"peer_public_key": %q,
			"mtu": 1350
		}]
	}`, priv, pub))}

	cfg := sbBuildJSON(t, profile, ConnectParams{})
	for _, raw := range sbRequireArray(t, cfg, "outbounds") {
		out := sbRequireObjectValue(t, raw, "outbound")
		if typ, _ := out["type"].(string); typ == "wireguard" {
			t.Fatal("legacy WireGuard outbound survived migration; sing-box >=1.13 removed it")
		}
	}
	endpoint := sbFindByStringField(t, sbRequireArray(t, cfg, "endpoints"), "type", "wireguard")
	if _, exists := endpoint["local_address"]; exists {
		t.Fatal("migrated endpoint still contains local_address")
	}
	if got := sbRequireStringArray(t, endpoint, "address"); !reflect.DeepEqual(got, []string{"10.0.0.2/32"}) {
		t.Fatalf("endpoint.address = %#v", got)
	}
	peer := sbRequireObjectValue(t, sbRequireArray(t, endpoint, "peers")[0], "endpoint.peers[0]")
	sbRequireString(t, peer, "address", freeTurnHost)
	sbRequireJSONInt(t, peer, "port", freeTurnPort)
	sbRequireString(t, peer, "public_key", pub)
	sbValidateGeneratedReferences(t, cfg)
}
