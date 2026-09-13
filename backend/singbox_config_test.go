package backend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetectConfigType(t *testing.T) {
	tests := []struct {
		name   string
		sb     string
		wgConf string
		want   ConfigType
	}{
		{
			name:   "JSON object unquoted",
			sb:     `{"outbounds":[]}`,
			wgConf: "",
			want:   ConfigTypeSingboxJSON,
		},
		{
			name:   "JSON object quoted",
			sb:     `"{\"outbounds\":[]}"`,
			wgConf: "",
			want:   ConfigTypeSingboxJSON,
		},
		{
			name:   "URI vless unquoted",
			sb:     `vless://uuid@host:443`,
			wgConf: "",
			want:   ConfigTypeURI,
		},
		{
			name:   "URI vless quoted",
			sb:     `"vless://uuid@host:443"`,
			wgConf: "",
			want:   ConfigTypeURI,
		},
		{
			name:   "WG Config fallback",
			sb:     "",
			wgConf: "[Interface]\nPrivateKey=...",
			want:   ConfigTypeWG,
		},
		{
			name:   "Unknown config",
			sb:     `"random_text"`,
			wgConf: "",
			want:   ConfigTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawSB := json.RawMessage(tt.sb)
			if tt.sb == "" {
				rawSB = nil
			}
			if got := DetectConfigType(rawSB, tt.wgConf); got != tt.want {
				t.Errorf("DetectConfigType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildSingboxConfigURI(t *testing.T) {
	prof := &ProfileData{
		SB: json.RawMessage(`"vless://00000000-0000-0000-0000-000000000000@1.2.3.4:443?type=ws&security=tls&sni=example.com"`),
	}
	params := ConnectParams{}

	cfgBytes, err := BuildSingboxConfig(prof, params)
	if err != nil {
		t.Fatalf("BuildSingboxConfig failed: %v", err)
	}

	cfgStr := string(cfgBytes)
	if !strings.Contains(cfgStr, `"type": "vless"`) {
		t.Errorf("Expected 'vless' outbound, got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, `"server": "127.0.0.1"`) {
		t.Errorf("Expected outbound to connect to 127.0.0.1, got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, `"server_port": 9000`) {
		t.Errorf("Expected outbound to connect to port 9000, got:\n%s", cfgStr)
	}
}

func TestBuildSingboxConfigWG(t *testing.T) {
	prof := &ProfileData{
		WGConfig: "[Interface]\nAddress = 10.0.0.2/32\nPrivateKey = privkey\n\n[Peer]\nPublicKey = pubkey\nEndpoint = 1.2.3.4:51820\nAllowedIPs = 0.0.0.0/0",
	}
	params := ConnectParams{MTU: 1350}

	cfgBytes, err := BuildSingboxConfig(prof, params)
	if err != nil {
		t.Fatalf("BuildSingboxConfig failed: %v", err)
	}

	cfgStr := string(cfgBytes)
	if !strings.Contains(cfgStr, `"type": "wireguard"`) {
		t.Errorf("Expected 'wireguard' endpoint, got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, `"address": "127.0.0.1"`) {
		t.Errorf("Expected WG endpoint to connect to 127.0.0.1, got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, `"port": 9000`) {
		t.Errorf("Expected WG endpoint to connect to port 9000, got:\n%s", cfgStr)
	}
}
