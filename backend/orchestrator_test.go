package backend

import "testing"

func TestClassifyLevel(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		// INFO — ничего особенного
		{"connection established", "INFO"},
		{"status running", "INFO"},
		{"tunnel active", "INFO"},
		{"peer connected 1.2.3.4:56000", "INFO"},
		{" WireGuard config applied", "INFO"},

		// ERROR
		{"FATAL_AUTH: неверный пароль", "ERROR"},
		{"FATAL_AUTH: access denied by server", "ERROR"},
		{"something error happened", "ERROR"},
		{"Фатальная ошибка: нет доступа", "ERROR"},
		{"ошибка подключения к серверу", "ERROR"},
		{"fatal: connection lost", "ERROR"},

		// WARN — retry / не удалось
		{"не удалось подключиться, повторим через 5s", "WARN"},
		{"повторим попытку через 3 секунды", "WARN"},
		{"retry #3 — reconnecting", "WARN"},
		{"retry attempt 1/5", "WARN"},
		{"не удалось отправить пакет", "WARN"},
		{"повторяем запрос к TURN серверу", "WARN"},

		// DEBUG — obfs / wrap / unwrap
		{"obfs: wrapping packet, len=1200", "DEBUG"},
		{"obfs: handshake init sent", "DEBUG"},
		{"obfs: session established", "DEBUG"},
		{"obfs: keepalive sent", "DEBUG"},
		{"obfs: padding added, total=1420", "DEBUG"},
		{"unwrap: decoded 1400 bytes", "DEBUG"},
		{"unwrap: session key rotated", "DEBUG"},
		{"wrap: encoded packet type=data", "DEBUG"},
		{"wrap: adding random padding 64 bytes", "DEBUG"},
		{"debug: buffer pool stats active=12 idle=4", "DEBUG"},
		{"obfs: timeout, retrying obfs handshake", "WARN"},

		// Mixed case / partial matches
		{"OBS error occurred", "ERROR"},
		{"DEBUG: obfs init", "DEBUG"},
		{"WARN retry next", "WARN"},
		{"obfs unwrap sequence", "DEBUG"},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := classifyLevel(tt.msg)
			if got != tt.want {
				t.Errorf("classifyLevel(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestIsRoutineSingboxTCPClosure(t *testing.T) {
	routine := []string{
		"[SB] ERROR connection: connection download closed: raw-read tcp 10.0.0.2:1234->1.2.3.4:443: An existing connection was forcibly closed by the remote host.",
		"[SB] ERROR connection: connection upload closed: write tcp4 172.19.0.1:1234->172.19.0.2:5678: wsasend: An established connection was aborted by the software in your host machine.",
		"[SB] ERROR connection: connection download closed: read tcp: connection reset by peer",
		"[SB] ERROR connection: connection upload closed: write tcp: broken pipe",
	}
	for _, msg := range routine {
		if !isRoutineSingboxTCPClosure(msg) {
			t.Errorf("routine close not recognized: %q", msg)
		}
	}

	realFailures := []string{
		"[SB] ERROR connection: open connection to 81.163.17.245:443 using outbound/direct[direct]: dial tcp 81.163.17.245:443: i/o timeout",
		"[SB] ERROR endpoint/wireguard[proxy]: failed to send handshake initiation",
		"[SB] ERROR tls: handshake failed: certificate verify failed",
		"[SB] ERROR inbound/tun[tun-in]: configure tun interface: access is denied",
		"[SB] FATAL start service: bind failed",
		"connection download closed: connection reset by peer", // not tagged as sing-box
	}
	for _, msg := range realFailures {
		if isRoutineSingboxTCPClosure(msg) {
			t.Errorf("real failure incorrectly downgraded: %q", msg)
		}
	}
}
