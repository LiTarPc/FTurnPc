package backend

import "testing"

func TestTrafficCounterSessionBaseline(t *testing.T) {
	var c trafficCounter
	if got := c.update(400*1024*1024, true); got != 0 {
		t.Fatalf("first sample = %d, want session baseline 0", got)
	}
	if got := c.update(400*1024*1024+128*1024, true); got != 128*1024 {
		t.Fatalf("second sample = %d, want %d", got, 128*1024)
	}
}

func TestTrafficCounterWindows32Wrap(t *testing.T) {
	var c trafficCounter
	const before = int64(0xfffffff0)
	if got := c.update(before, true); got != 0 {
		t.Fatalf("baseline = %d, want 0", got)
	}
	if got := c.update(0x20, true); got != 0x30 {
		t.Fatalf("wrapped delta total = %d, want %d", got, 0x30)
	}
}

func TestTrafficCounterInterfaceReset(t *testing.T) {
	var c trafficCounter
	_ = c.update(1000, true)
	if got := c.update(1500, true); got != 500 {
		t.Fatalf("pre-reset total = %d, want 500", got)
	}
	if got := c.update(100, true); got != 600 {
		t.Fatalf("post-reset total = %d, want 600", got)
	}
	if got := c.update(160, true); got != 660 {
		t.Fatalf("continued total = %d, want 660", got)
	}
}
