package backend

import "testing"

func TestIsFreeTurnReadyLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "dtls ready", line: "Established DTLS connection", want: true},
		{name: "stream ready", line: "[STREAM 3] stream is ready", want: true},
		{name: "active equals one", line: "activeConnectionCount=1", want: true},
		{name: "active colon two", line: "activeConnectionCount: 2", want: true},
		{name: "active json three", line: `{"activeConnectionCount":3}`, want: true},
		{name: "active zero", line: "activeConnectionCount=0", want: true},
		{name: "active json zero", line: `{"activeConnectionCount":0}`, want: true},
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

	e.trackFreeTurnStream("[STREAM 7] relayed-address=1.2.3.4")
	if !e.activeStreams["7"] {
		t.Fatal("stream 7 should be active after relayed-address")
	}

	e.trackFreeTurnStream("[STREAM 7] closed")
	if e.activeStreams["7"] {
		t.Fatal("stream 7 should be removed after close")
	}

	e.trackFreeTurnStream("[STREAM ] stream is ready")
	if _, exists := e.activeStreams[""]; exists {
		t.Fatal("empty stream id must not be tracked")
	}
}
