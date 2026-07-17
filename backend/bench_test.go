package backend

import (
	"fmt"
	"strings"
	"testing"
)

var benchWGConfig = `[Interface]
Address = 10.0.0.2/32
DNS = 1.1.1.1, 8.8.8.8
MTU = 1420
PrivateKey = very-long-private-key-base64-==
PostUp = iptables -A FORWARD -i wg-turn -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PreDown = iptables -D FORWARD -i wg-turn -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE

[Peer]
PublicKey = another-long-base64-key==
Endpoint = 185.16.28.10:56001
AllowedIPs = 0.0.0.0/0, ::/0, 87.240.128.0/18, 90.156.0.0/16, 77.88.0.0/18
PersistentKeepalive = 25
`

func BenchmarkParseWGConfig(b *testing.B) {
	for b.Loop() {
		parseWGConfig(benchWGConfig)
	}
}

func BenchmarkParseWGConfig_LargeConfig(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("[Interface]\nAddress = 10.0.0.2/32\nDNS = 1.1.1.1\nMTU = 1420\nPrivateKey = secret\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "PostUp = echo %d\n", i)
	}
	sb.WriteString("[Peer]\nPublicKey = pub\nEndpoint = 1.2.3.4:51820\n")
	var ips []string
	for i := 0; i < 100; i++ {
		ips = append(ips, fmt.Sprintf("%d.%d.%d.0/24", i%256, (i/256)%256, (i/65536)%256))
	}
	fmt.Fprintf(&sb, "AllowedIPs = %s\n", strings.Join(ips, ", "))
	config := sb.String()
	b.ResetTimer()
	for b.Loop() {
		parseWGConfig(config)
	}
}

func BenchmarkClassifyLevel(b *testing.B) {
	msgs := []string{
		"connection established to 1.2.3.4",
		"FATAL_AUTH: неверный пароль",
		"retry #3 reconnecting",
		"obfs: wrapping packet len=1200",
		"не удалось подключиться",
		"unwrap: decoded 1400 bytes",
	}
	for b.Loop() {
		for _, m := range msgs {
			classifyLevel(m)
		}
	}
}

func BenchmarkShellQuote(b *testing.B) {
	for b.Loop() {
		shellQuote("simple-string-without-quotes")
	}
}

func BenchmarkShellQuote_WithQuotes(b *testing.B) {
	for b.Loop() {
		shellQuote("it's a 'complicated' string with \"many\" quotes")
	}
}

func BenchmarkWgIfaceConst(b *testing.B) {
	for b.Loop() {
		_ = wgIface
	}
}
