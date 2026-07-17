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

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", "'it'\"'\"'s'"},
		{"", "''"},
		{"a b c", "'a b c'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
