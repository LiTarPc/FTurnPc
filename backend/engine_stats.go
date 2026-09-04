package backend

import (
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// startStatsLoop запускает фоновый опрос объема сетевого трафика и количества активных потоков.
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

		var lastRx, lastTx int64
		var cumRx, cumTx int64

		for {
			select {
			case <-t.C:
				rx, tx, err := getInterfaceBytes(wgIface)
				if err != nil {
					continue
				}

				// Компенсация 32-битного переполнения (Windows GetIfEntry возвращает uint32)
				if rx < lastRx {
					if lastRx > 0x80000000 && rx < 0x40000000 {
						// Реальное переполнение uint32 (> 4 ГБ)
						cumRx += (1 << 32)
					} else {
						// Сброс счетчика сетевого адаптера (реконнект, переподнятие интерфейса)
						cumRx += lastRx
					}
				}
				if tx < lastTx {
					if lastTx > 0x80000000 && tx < 0x40000000 {
						cumTx += (1 << 32)
					} else {
						cumTx += lastTx
					}
				}
				lastRx = rx
				lastTx = tx

				realRx := cumRx + rx
				realTx := cumTx + tx

				e.muStreams.Lock()
				activeCount := len(e.activeStreams)
				e.muStreams.Unlock()

				packedWorkers := int32(activeCount) | (int32(e.configuredStreams) << 16)

				if e.onTray != nil {
					e.onTray(true, realRx, realTx, packedWorkers)
				}
				runtime.EventsEmit(e.appCtx, "stats", map[string]interface{}{
					"rx":             realRx,
					"tx":             realTx,
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
