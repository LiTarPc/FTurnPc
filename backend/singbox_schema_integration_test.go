//go:build integration

package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Integration tests must use the exact sing-box build shipped with the app.
// Example:
//
//	SING_BOX_BIN=C:\\path\\to\\sing-box.exe go test -tags=integration ./backend -run TestSingBoxSchema -v
//
// Do not silently use an arbitrary PATH binary in CI: schema compatibility is
// only meaningful against the release binary.
func TestSingBoxSchema_GeneratedConfigMatrix(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("SING_BOX_BIN"))
	if bin == "" {
		t.Fatal("SING_BOX_BIN is required for -tags=integration tests")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("SING_BOX_BIN %q is not usable: %v", bin, err)
	}

	version := sbIntegrationCommand(t, bin, "version")
	if want := strings.TrimSpace(os.Getenv("SING_BOX_EXPECTED_VERSION")); want != "" {
		if !strings.Contains(version, want) {
			t.Fatalf("sing-box version mismatch: got %q, expected substring %q", version, want)
		}
	} else if !strings.Contains(version, "1.14.") {
		t.Fatalf("integration suite currently targets sing-box 1.14.x; got %q; set SING_BOX_EXPECTED_VERSION when intentionally upgrading", version)
	}
	t.Logf("schema validation binary: %s", strings.TrimSpace(version))

	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:secret-password"))

	tests := []struct {
		name    string
		profile *ProfileData
		params  ConnectParams
	}{
		{
			name: "vless tls ws",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"vless://00000000-0000-0000-0000-000000000001@origin.example:443?security=tls&sni=origin.example&type=ws&host=cdn.example&path=%2Fws")},
		},
		{
			name: "trojan tls",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"trojan://secret@origin.example:443?security=tls&sni=origin.example")},
		},
		{
			name: "shadowsocks SIP002",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"ss://"+ssUser+"@origin.example:8388")},
		},
		{
			name: "hysteria2",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"hysteria2://secret@origin.example:443?sni=origin.example")},
		},
		{
			name: "hy2 alias",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"hy2://secret@origin.example:443?sni=origin.example")},
		},
		{
			name: "tuic",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"tuic://00000000-0000-0000-0000-000000000001:secret@origin.example:443?sni=origin.example")},
		},
		{
			name:    "wireguard endpoint",
			profile: &ProfileData{WGConfig: sbIntegrationWGConfig()},
			params:  ConnectParams{MTU: 1350},
		},
		{
			name: "ready JSON tagged",
			profile: &ProfileData{SB: json.RawMessage(`{
				"outbounds": [{
					"type": "vless",
					"tag": "custom-proxy",
					"server": "127.0.0.1",
					"server_port": 9000,
					"uuid": "00000000-0000-0000-0000-000000000001"
				}]
			}`)},
		},
		{
			name: "ready JSON untagged",
			profile: &ProfileData{SB: json.RawMessage(`{
				"outbounds": [{
					"type": "vless",
					"server": "127.0.0.1",
					"server_port": 9000,
					"uuid": "00000000-0000-0000-0000-000000000001"
				}]
			}`)},
		},
		{
			name: "legacy wireguard outbound migration",
			profile: &ProfileData{SB: json.RawMessage(fmt.Sprintf(`{
				"outbounds": [{
					"type": "wireguard",
					"tag": "proxy",
					"local_address": ["10.0.0.2/32"],
					"private_key": %q,
					"peer_public_key": %q,
					"server": "old.example",
					"server_port": 51820
				}]
			}`, sbIntegrationWGKey(0x11), sbIntegrationWGKey(0x22)))},
		},
		{
			name: "bypass ru inline fallback",
			profile: &ProfileData{SB: sbIntegrationRawString(t,
				"vless://00000000-0000-0000-0000-000000000001@origin.example:443?security=tls&sni=origin.example")},
			params: ConnectParams{BypassRu: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := BuildSingboxConfig(tt.profile, tt.params)
			if err != nil {
				t.Fatalf("BuildSingboxConfig: %v", err)
			}

			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, cfg, 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "check", "-c", path)
			out, err := cmd.CombinedOutput()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("sing-box check timed out after 5s")
			}
			if err != nil {
				t.Fatalf("sing-box check failed: %v\n%s\nGenerated config:\n%s", err, out, cfg)
			}
		})
	}
}

func sbIntegrationCommand(t *testing.T, bin string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("%s %v timed out", bin, args)
	}
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", bin, args, err, out)
	}
	return string(out)
}

func sbIntegrationRawString(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal(%q): %v", s, err)
	}
	return b
}

func sbIntegrationWGConfig() string {
	return fmt.Sprintf(`[Interface]
Address = 10.0.0.2/32
PrivateKey = %s
MTU = 1350
DNS = 1.1.1.1

[Peer]
PublicKey = %s
Endpoint = original.example:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`, sbIntegrationWGKey(0x11), sbIntegrationWGKey(0x22))
}

func sbIntegrationWGKey(fill byte) string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = fill
	}
	return base64.StdEncoding.EncodeToString(b)
}
