package backend

import "testing"

func TestTrafficTotalsCountFirstTickFromImmediateBaseline(t *testing.T) {
	totals := newTrafficTotals(10_000, 20_000)
	rx, tx := totals.update(10_750, 21_250)
	if rx != 750 || tx != 1_250 {
		t.Fatalf("first tick totals = (%d, %d), want (750, 1250)", rx, tx)
	}
}

func TestTrafficCounterSessionBaseline(t *testing.T) {
	var c trafficCounter
	if got := c.update(400 * 1024 * 1024); got != 0 {
		t.Fatalf("first sample = %d, want session baseline 0", got)
	}
	if got := c.update(400*1024*1024 + 128*1024); got != 128*1024 {
		t.Fatalf("second sample = %d, want %d", got, 128*1024)
	}
}

func TestTrafficCounterHandlesValuesPast32Bit(t *testing.T) {
	var c trafficCounter
	const baseline = int64(5) << 30 // 5 GiB, safely beyond uint32 range.
	if got := c.update(baseline); got != 0 {
		t.Fatalf("baseline = %d, want 0", got)
	}
	if got := c.update(baseline + 64*1024*1024); got != 64*1024*1024 {
		t.Fatalf("64-bit delta total = %d, want %d", got, 64*1024*1024)
	}
}

func TestTrafficCounterInterfaceReset(t *testing.T) {
	var c trafficCounter
	_ = c.update(5 << 30)
	if got := c.update((5 << 30) + 500); got != 500 {
		t.Fatalf("pre-reset total = %d, want 500", got)
	}
	if got := c.update(100); got != 600 {
		t.Fatalf("post-reset total = %d, want 600", got)
	}
	if got := c.update(160); got != 660 {
		t.Fatalf("continued total = %d, want 660", got)
	}
}
