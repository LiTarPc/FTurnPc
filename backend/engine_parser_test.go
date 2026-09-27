package backend

import "testing"

func TestIsFreeTurnReadyLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		// An allocation is not a usable DTLS transport yet.
		{name: "udp allocation pending", line: "[STREAM 1] TURN allocation up: relayed=91.231.135.171:60989 server=91.231.135.171", want: false},

		// Current FreeTurn TCP mode emits these at INFO level after a usable
		// KCP/smux session has joined the pool.
		{name: "tcp session connected", line: "[session 2] connected (active: 1)", want: true},
		{name: "tcp pool serving", line: "TCP mode: pool serving traffic (active: 1)", want: true},
		{name: "tcp listener alone is not ready", line: "TCP mode: listening on 127.0.0.1:9000 (round-robin across 10 sessions)", want: false},
		{name: "tcp waiting alone is not ready", line: "TCP mode: waiting for sessions to connect (total: 10)...", want: false},

		// Compatibility with older or debug builds.
		{name: "dtls ready", line: "Established DTLS connection", want: true},
		{name: "stream ready", line: "[STREAM 3] stream is ready", want: true},
		{name: "active equals one", line: "activeConnectionCount=1", want: true},
		{name: "active colon two", line: "activeConnectionCount: 2", want: true},
		{name: "active json three", line: `{"activeConnectionCount":3}`, want: true},

		// Zero connections must never advance the UI to running.
		{name: "active zero", line: "activeConnectionCount=0", want: false},
		{name: "active json zero", line: `{"activeConnectionCount":0}`, want: false},
		{name: "allocation released", line: "[STREAM 1] TURN allocation released: relayed=1.2.3.4:1000 deallocate=<nil>", want: false},
		{name: "active without value", line: "activeConnectionCount", want: false},
		{name: "unrelated", line: "connecting to TURN", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFreeTurnReadyLine(tt.line); got != tt.want {
				t.Fatalf("isFreeTurnReadyLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestTrackFreeTurnStream(t *testing.T) {
	e := &FreeturnEngine{activeStreams: make(map[string]bool)}

	// UDP mode: the production INFO line is TURN allocation up/released.
	e.trackFreeTurnStream("[STREAM 7] TURN allocation up: relayed=1.2.3.4:12345 server=1.2.3.4")
	if !e.activeStreams["7"] {
		t.Fatal("UDP stream 7 should be active after TURN allocation up")
	}
	e.trackFreeTurnStream("[STREAM 7] TURN allocation released: relayed=1.2.3.4:12345 deallocate=<nil>")
	if e.activeStreams["7"] {
		t.Fatal("UDP stream 7 should be removed after allocation release")
	}

	// TCP mode uses [session N] rather than [STREAM N].
	e.trackFreeTurnStream("[session 3] connected (active: 1)")
	if !e.activeStreams["3"] {
		t.Fatal("TCP session 3 should be active after connected")
	}
	e.trackFreeTurnStream("[session 3] disconnected (active: 0), reconnecting...")
	if e.activeStreams["3"] {
		t.Fatal("TCP session 3 should be removed after disconnected")
	}

	// Old/debug formats remain supported.
	e.trackFreeTurnStream("[STREAM 8] relayed-address=1.2.3.4")
	if !e.activeStreams["8"] {
		t.Fatal("legacy stream 8 should be active after relayed-address")
	}
	e.trackFreeTurnStream("[STREAM 8] closed")
	if e.activeStreams["8"] {
		t.Fatal("legacy stream 8 should be removed after close")
	}

	e.trackFreeTurnStream("[STREAM ] stream is ready")
	if _, exists := e.activeStreams[""]; exists {
		t.Fatal("empty stream id must not be tracked")
	}
}

func TestFreeTurnStreamID(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"[STREAM 10] TURN allocation up", "10"},
		{"prefix [session 4] connected (active: 2)", "4"},
		{"[STREAM ] broken", ""},
		{"no id here", ""},
	}
	for _, tt := range tests {
		if got := freeTurnStreamID(tt.line); got != tt.want {
			t.Fatalf("freeTurnStreamID(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFreeTurnServerIP(t *testing.T) {
	tests := []struct{ line, want string }{
		{"[STREAM 1] TURN allocation up: relayed=91.231.135.171:38041 server=91.231.135.171", "91.231.135.171"},
		{"Resolved TURN server vk.example to 193.203.43.16:19302", "193.203.43.16"},
		{"TURN server IP: 193.203.43.16", "193.203.43.16"},
		{"selected turn: 91.231.135.171:19302", "91.231.135.171"},
		{"[STREAM 2] TURN allocation released: relayed=193.203.43.16:65105", ""},
		{"TURN allocation up: server=127.0.0.1", ""},
		{"TURN allocation up: server=not-an-ip", ""},
	}
	for _, tt := range tests {
		if got := freeTurnServerIP(tt.line); got != tt.want {
			t.Errorf("freeTurnServerIP(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestSafeFreeTurnLogLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"[STREAM 1] Established DTLS connection", true},
		{"[STREAM 1] [Captcha] solver succeeded", true},
		{"[STREAM 1] [Captcha] pow envelope: {\"hash\":\"secret\"}", false},
		{"[Captcha Proxy] real browser pow: {\"nonce\":1}", false},
		{"[Captcha] header: Cookie = session=value", false},
		{"[turnc] request session_token=secret", false},
	}
	for _, tt := range tests {
		if got := safeFreeTurnLogLine(tt.line); got != tt.want {
			t.Errorf("safeFreeTurnLogLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}
