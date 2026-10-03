package backend

import (
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// trafficCounter converts an absolute interface byte counter into bytes used
// since this stats loop started. Interface counters can survive between user
// sessions, so the first successful sample is always treated as the baseline.
type trafficCounter struct {
	initialized bool
	last        int64
	total       int64
}

type trafficTotals struct {
	rx trafficCounter
	tx trafficCounter
}

func newTrafficTotals(rx, tx int64) trafficTotals {
	var totals trafficTotals
	totals.update(rx, tx)
	return totals
}

func (t *trafficTotals) update(rx, tx int64) (sessionRx, sessionTx int64) {
	return t.rx.update(rx), t.tx.update(tx)
}

func (c *trafficCounter) update(raw int64) int64 {
	if raw < 0 {
		return c.total
	}
	if !c.initialized {
		c.initialized = true
		c.last = raw
		return 0
	}

	var delta int64
	if raw >= c.last {
		delta = raw - c.last
	} else {
		// The TUN adapter was reset/recreated and its absolute counter restarted.
		// Keep the accumulated session total and count only bytes observed after
		// the reset. Windows uses GetIfEntry2 64-bit octet counters, so there is
		// no 32-bit wraparound to compensate for here.
		delta = raw
	}

	if delta > 0 {
		c.total += delta
	}
	c.last = raw
	return c.total
}

// startStatsLoop polls byte counters from the fturn-tun adapter and reports
// traffic that crossed that interface during the current user connection.
func (e *FreeturnEngine) startStatsLoop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.statsStop != nil {
		return
	}

	// Capture the baseline before the first one-second tick. Waiting for the
	// ticker here used to discard every byte transferred during the first
	// second of a connection.
	tunName := e.tunName
	if tunName == "" {
		tunName = singTunName
	}
	initialRx, initialTx, initialErr := getInterfaceBytes(tunName)
	var totals trafficTotals
	if initialErr == nil {
		totals = newTrafficTotals(initialRx, initialTx)
	}
	e.statsStop = make(chan struct{})
	go func(stop chan struct{}, totals trafficTotals) {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()

		for {
			select {
			case <-t.C:
				rx, tx, err := getInterfaceBytes(tunName)
				if err != nil {
					continue
				}

				sessionRx, sessionTx := totals.update(rx, tx)

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
	}(e.statsStop, totals)
}

func (e *FreeturnEngine) stopStatsLoopLocked() {
	if e.statsStop != nil {
		close(e.statsStop)
		e.statsStop = nil
	}
}
