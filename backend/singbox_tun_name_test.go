package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSessionTunName(t *testing.T) {
	first := newSessionTunName()
	second := newSessionTunName()
	if runtime.GOOS == "windows" {
		if first == second || !strings.HasPrefix(first, "fturn-") || !strings.HasPrefix(second, "fturn-") {
			t.Fatalf("Windows TUN names should be distinct fturn names: %q, %q", first, second)
		}
	} else if first != singTunName || second != singTunName {
		t.Fatalf("non-Windows TUN names changed: %q, %q", first, second)
	}
}

func TestTunAdapterCollisionDetection(t *testing.T) {
	if !isTunAdapterCollision(errors.New("sing-box ошибка: configure tun interface: (create adapter: Cannot create a file when that file already exists. | open existing adapter: Element not found.)")) {
		t.Fatal("missed the observed Wintun collision")
	}
	if isTunAdapterCollision(errors.New("configure tun interface: access is denied")) {
		t.Fatal("retrying an unrelated TUN error")
	}
}

func TestReplaceSessionTunNamePreservesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"inbounds":[{"type":"tun","interface_name":"fturn-old"}],"dns":{"final":"dns-remote"},"route":{"final":"proxy"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceSessionTunName(path, "fturn-new"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	inbounds := cfg["inbounds"].([]interface{})
	if got := inbounds[0].(map[string]interface{})["interface_name"]; got != "fturn-new" {
		t.Fatalf("TUN interface_name = %v", got)
	}
	if cfg["dns"].(map[string]interface{})["final"] != "dns-remote" || cfg["route"].(map[string]interface{})["final"] != "proxy" {
		t.Fatal("retry changed DNS or routing")
	}
}

func TestBuildSingboxConfigUsesSessionTunName(t *testing.T) {
	profile := &ProfileData{WGConfig: fmt.Sprintf(`[Interface]
Address = 10.0.0.2/32
PrivateKey = %s

[Peer]
PublicKey = %s
AllowedIPs = 0.0.0.0/0
`, sbWGKey(0x11), sbWGKey(0x22))}
	cfg := sbBuildJSON(t, profile, ConnectParams{tunName: "fturn-test123"})
	tun := sbFindByStringField(t, sbRequireArray(t, cfg, "inbounds"), "type", "tun")
	sbRequireString(t, tun, "interface_name", "fturn-test123")
}
