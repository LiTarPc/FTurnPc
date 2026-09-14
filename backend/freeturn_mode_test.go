package backend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveFreeTurnMode_Matrix(t *testing.T) {
	tests := []struct {
		name    string
		profile *ProfileData
		want    string
	}{
		{name: "legacy WireGuard", profile: &ProfileData{WGConfig: "[Interface]\nAddress=10.0.0.2/32"}, want: freeTurnModeUDP},
		{name: "VLESS", profile: &ProfileData{SB: modeRaw(t, "vless://id@example.com:443")}, want: freeTurnModeTCP},
		{name: "Trojan", profile: &ProfileData{SB: modeRaw(t, "trojan://secret@example.com:443")}, want: freeTurnModeTCP},
		{name: "Shadowsocks", profile: &ProfileData{SB: modeRaw(t, "ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388")}, want: freeTurnModeTCP},
		{name: "Hysteria2", profile: &ProfileData{SB: modeRaw(t, "hysteria2://secret@example.com:443")}, want: freeTurnModeUDP},
		{name: "hy2 alias", profile: &ProfileData{SB: modeRaw(t, "hy2://secret@example.com:443")}, want: freeTurnModeUDP},
		{name: "TUIC", profile: &ProfileData{SB: modeRaw(t, "tuic://id:secret@example.com:443")}, want: freeTurnModeUDP},
		{name: "JSON VLESS", profile: &ProfileData{SB: json.RawMessage(`{"outbounds":[{"type":"vless","tag":"proxy"}]}`)}, want: freeTurnModeTCP},
		{name: "JSON WireGuard endpoint", profile: &ProfileData{SB: json.RawMessage(`{"endpoints":[{"type":"wireguard","tag":"proxy"}]}`)}, want: freeTurnModeUDP},
		{name: "legacy JSON WireGuard outbound", profile: &ProfileData{SB: json.RawMessage(`{"outbounds":[{"type":"wireguard","tag":"proxy"}]}`)}, want: freeTurnModeUDP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFreeTurnMode(tt.profile)
			if err != nil {
				t.Fatalf("ResolveFreeTurnMode: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveFreeTurnMode_ExplicitModeValidation(t *testing.T) {
	profile := &ProfileData{Mode: "tcp", SB: modeRaw(t, "vless://id@example.com:443")}
	if got, err := ResolveFreeTurnMode(profile); err != nil || got != "tcp" {
		t.Fatalf("matching explicit mode: got=%q err=%v", got, err)
	}

	profile.Mode = "udp"
	if _, err := ResolveFreeTurnMode(profile); err == nil || !strings.Contains(strings.ToLower(err.Error()), "конфликт") {
		t.Fatalf("conflicting explicit mode must fail, got %v", err)
	}

	profile.Mode = "quic"
	if _, err := ResolveFreeTurnMode(profile); err == nil {
		t.Fatal("unknown explicit mode was accepted")
	}
}

func TestResolveFreeTurnMode_ExplicitModeCanCoverUnknownJSONProtocol(t *testing.T) {
	profile := &ProfileData{
		Mode: "tcp",
		SB:   json.RawMessage(`{"outbounds":[{"type":"custom-protocol","tag":"proxy"}]}`),
	}
	got, err := ResolveFreeTurnMode(profile)
	if err != nil || got != "tcp" {
		t.Fatalf("explicit mode should cover an intentionally unsupported inference: got=%q err=%v", got, err)
	}
}

func TestBuildFreeTurnArgs_ModeAndTransportAreIndependent(t *testing.T) {
	profile := &ProfileData{
		PeerAddr:       "203.0.113.1:3478",
		Transport:      "udp",
		Links:          "https://example.invalid/join/token",
		Power:          7,
		StreamsPerCred: 3,
		Obf:            "profile",
		Key:            "secret-key",
		Cid:            "client-id",
	}
	args := buildFreeTurnArgs(ConnectParams{}, profile, freeTurnModeTCP)
	assertArgPair(t, args, "-mode", "tcp")
	assertArgPair(t, args, "-transport", "udp")
	assertArgPair(t, args, "-listen", "127.0.0.1:9000")
	assertArgPair(t, args, "-peer", profile.PeerAddr)
	assertArgPair(t, args, "-n", "7")
	assertArgPair(t, args, "-streams-per-cred", "3")
	for _, arg := range args {
		if arg == "-debug" {
			t.Fatal("production FreeTurn args must not enable -debug unconditionally")
		}
	}
}

func TestRedactFreeTurnArgs_HidesSecrets(t *testing.T) {
	args := []string{"-peer", "203.0.113.1:3478", "-links", "secret-link", "-obf-key", "secret-key", "-client-id", "secret-id"}
	redacted := redactFreeTurnArgs(args)
	for _, secret := range []string{"secret-link", "secret-key", "secret-id"} {
		for _, value := range redacted {
			if value == secret {
				t.Fatalf("secret %q leaked in redacted args: %#v", secret, redacted)
			}
		}
	}
	if strings.Join(args, " ") == strings.Join(redacted, " ") {
		t.Fatalf("redaction did not change args: %#v", redacted)
	}
}

func modeRaw(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertArgPair(t *testing.T, args []string, key, want string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			if args[i+1] != want {
				t.Fatalf("%s = %q, want %q; args=%#v", key, args[i+1], want, args)
			}
			return
		}
	}
	t.Fatalf("argument %s not found in %#v", key, args)
}
