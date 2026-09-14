package backend

import (
	goruntime "runtime"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// trafficCounter converts an absolute interface byte counter into bytes used
// since this stats loop started. This is important on Windows because the TUN
// adapter can survive between sessions and GetIfEntry reports lifetime adapter
// counters rather than per-connection counters.
type trafficCounter struct {
	initialized bool
	last        int64
	total       int64
}

func (c *trafficCounter) update(raw int64, windows32 bool) int64 {
	if raw < 0 {
		return c.total
	}
	if !c.initialized {
		c.initialized = true
		c.last = raw
		return 0
	}

	var delta int64
	switch {
	case raw >= c.last:
		delta = raw - c.last
	case windows32 && c.last > 0x80000000 && raw < 0x40000000:
		// Legacy GetIfEntry exposes 32-bit octet counters. Handle a real
		// uint32 wrap without turning it into a multi-gigabyte UI spike.
		delta = (1 << 32) - c.last + raw
	default:
		// The interface counter was reset/recreated. Keep the accumulated
		// session total and count only bytes observed after the reset.
		delta = raw
	}

	if delta > 0 {
		c.total += delta
	}
	c.last = raw
	return c.total
}

// startStatsLoop polls the TUN byte counters and active FreeTurn streams.
func (e *FreeturnEngine) startStatsLoop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.statsStop != nil {
		return
	}
	e.statsStop = make(chan struct{})
	go func(stop chan struct{}) {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()

		var rxCounter, txCounter trafficCounter
		windows32 := goruntime.GOOS == "windows"

		for {
			select {
			case <-t.C:
				rx, tx, err := getInterfaceBytes(singTunName)
				if err != nil {
					continue
				}

				sessionRx := rxCounter.update(rx, windows32)
				sessionTx := txCounter.update(tx, windows32)

				e.muStreams.Lock()
				activeCount := len(e.activeStreams)
				e.muStreams.Unlock()

				packedWorkers := int32(activeCount) | (int32(e.configuredStreams) << 16)

				if e.onTray != nil {
					e.onTray(true, sessionRx, sessionTx, packedWorkers)
				}
				runtime.EventsEmit(e.appCtx, "stats", map[string]interface{}{
					"rx":             sessionRx,
					"tx":             sessionTx,
					"active_streams": activeCount,
					"configured_max": e.configuredStreams,
				})
			case <-stop:
				return
			}
		}
	}(e.statsStop)
}

func (e *FreeturnEngine) stopStatsLoopLocked() {
	if e.statsStop != nil {
		close(e.statsStop)
		e.statsStop = nil
	}
}
