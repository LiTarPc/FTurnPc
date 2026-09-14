package backend

import (
	"encoding/json"
	"testing"
)

func TestHardenSingboxLoopbackConfig_WireGuardEndpoint(t *testing.T) {
	input := []byte(`{
		"inbounds":[{
			"type":"tun",
			"tag":"tun-in",
			"route_exclude_address":["10.0.0.0/8"]
		}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"endpoints":[{
			"type":"wireguard",
			"tag":"proxy",
			"address":["10.0.0.2/32"],
			"private_key":"test",
			"peers":[{
				"address":"127.0.0.1",
				"port":9000,
				"public_key":"peer"
			}]
		}],
		"route":{"auto_detect_interface":true,"final":"proxy"}
	}`)

	out, err := HardenSingboxLoopbackConfig(input)
	if err != nil {
		t.Fatalf("HardenSingboxLoopbackConfig: %v", err)
	}
	cfg := decodeLoopbackTestJSON(t, out)

	tun := firstObject(t, cfg, "inbounds")
	excludes := stringArray(t, tun["route_exclude_address"])
	if !containsString(excludes, "10.0.0.0/8") {
		t.Fatalf("existing route exclusion was lost: %#v", excludes)
	}
	if !containsString(excludes, freeTurnLoopbackRoute) {
		t.Fatalf("loopback exclusion missing: %#v", excludes)
	}

	endpoint := firstObject(t, cfg, "endpoints")
	if got, _ := endpoint["inet4_bind_address"].(string); got != freeTurnHost {
		t.Fatalf("WireGuard endpoint inet4_bind_address = %q, want %q", got, freeTurnHost)
	}
}

func TestHardenSingboxLoopbackConfig_TCPAndUDPOutbounds(t *testing.T) {
	input := []byte(`{
		"inbounds":[{"type":"tun","tag":"tun-in"}],
		"outbounds":[
			{"type":"vless","tag":"proxy","server":"127.0.0.1","server_port":9000,"uuid":"x"},
			{"type":"direct","tag":"direct"},
			{"type":"vless","tag":"external","server":"203.0.113.10","server_port":443,"uuid":"y"}
		],
		"route":{"auto_detect_interface":true,"final":"proxy"}
	}`)

	out, err := HardenSingboxLoopbackConfig(input)
	if err != nil {
		t.Fatalf("HardenSingboxLoopbackConfig: %v", err)
	}
	cfg := decodeLoopbackTestJSON(t, out)
	outbounds, ok := cfg["outbounds"].([]interface{})
	if !ok || len(outbounds) != 3 {
		t.Fatalf("outbounds = %#v", cfg["outbounds"])
	}

	proxy := objectValue(t, outbounds[0])
	if got, _ := proxy["inet4_bind_address"].(string); got != freeTurnHost {
		t.Fatalf("localhost proxy inet4_bind_address = %q, want %q", got, freeTurnHost)
	}
	if _, exists := objectValue(t, outbounds[1])["inet4_bind_address"]; exists {
		t.Fatal("direct outbound unexpectedly received a loopback bind")
	}
	if _, exists := objectValue(t, outbounds[2])["inet4_bind_address"]; exists {
		t.Fatal("external outbound unexpectedly received a loopback bind")
	}
}

func TestHardenSingboxLoopbackConfig_IsIdempotent(t *testing.T) {
	input := []byte(`{
		"inbounds":[{"type":"tun","route_exclude_address":["127.0.0.0/8"]}],
		"outbounds":[{"type":"vless","server":"127.0.0.1","server_port":9000}]
	}`)

	once, err := HardenSingboxLoopbackConfig(input)
	if err != nil {
		t.Fatalf("first harden: %v", err)
	}
	twice, err := HardenSingboxLoopbackConfig(once)
	if err != nil {
		t.Fatalf("second harden: %v", err)
	}
	cfg := decodeLoopbackTestJSON(t, twice)
	tun := firstObject(t, cfg, "inbounds")
	excludes := stringArray(t, tun["route_exclude_address"])
	count := 0
	for _, item := range excludes {
		if item == freeTurnLoopbackRoute {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("loopback exclusion count = %d, want 1 (%#v)", count, excludes)
	}
}

func TestHardenSingboxLoopbackConfig_RejectsInvalidExcludeType(t *testing.T) {
	input := []byte(`{
		"inbounds":[{"type":"tun","route_exclude_address":123}],
		"outbounds":[]
	}`)
	if _, err := HardenSingboxLoopbackConfig(input); err == nil {
		t.Fatal("invalid route_exclude_address type was accepted")
	}
}

func decodeLoopbackTestJSON(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, data)
	}
	return cfg
}

func firstObject(t *testing.T, cfg map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	values, ok := cfg[key].([]interface{})
	if !ok || len(values) == 0 {
		t.Fatalf("%s = %#v", key, cfg[key])
	}
	return objectValue(t, values[0])
}

func objectValue(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	obj, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("value is %T, want object", value)
	}
	return obj
}

func stringArray(t *testing.T, value interface{}) []string {
	t.Helper()
	raw, ok := value.([]interface{})
	if !ok {
		t.Fatalf("value is %T, want array", value)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("array contains %T, want string", item)
		}
		out = append(out, s)
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
