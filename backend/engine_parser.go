package backend

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// parseLogs построчно читает вывод FreeTurn, отслеживает состояние потоков
// и детектирует системные события для запуска sing-box TUN.
func (e *FreeturnEngine) parseLogs(r io.Reader) {
	defer e.wg.Done()
	scanner := bufio.NewScanner(r)
	var lastErrTime time.Time

	for scanner.Scan() {
		line := scanner.Text()

		// 1. Отслеживание состояния потоков ([STREAM 1] ...)
		if idx := strings.Index(line, "[STREAM "); idx != -1 {
			sub := line[idx+8:]
			if end := strings.Index(sub, "]"); end != -1 {
				streamID := sub[:end]
				e.muStreams.Lock()
				if e.activeStreams == nil {
					e.activeStreams = make(map[string]bool)
				}
				if strings.Contains(line, "relayed-address") || strings.Contains(line, "Established") || strings.Contains(line, "stream is ready") {
					e.activeStreams[streamID] = true
				}
				if strings.Contains(line, "closed") || strings.Contains(line, "failed") {
					delete(e.activeStreams, streamID)
				}
				e.muStreams.Unlock()
			}
		}

		// 2. Предупреждение об ошибке авторизации VK (поток автоматически повторит попытку)
		if strings.Contains(line, "all VK credentials failed") {
			runtime.EventsEmit(e.appCtx, "log", "WARN", "[SB] Ошибка получения токена VK для потока. Ожидание автоматической повторной попытки...")
		}

		// 3. Детекция запроса ручного ввода капчи (поддержка портов 8765 и 2212)
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "localhost:8765") || strings.Contains(lowerLine, "localhost:2212") ||
			strings.Contains(lowerLine, "127.0.0.1:8765") || strings.Contains(lowerLine, "127.0.0.1:2212") ||
			strings.Contains(lowerLine, "manual captcha") {
			runtime.EventsEmit(e.appCtx, "log", "WARNING", "[SB] Требуется ввод капчи. Ожидание действий пользователя (туннель не отключается)...")
		}

		// 4. Поднятие sing-box TUN при готовности DTLS сессии
		if strings.Contains(line, "Established DTLS connection") || strings.Contains(line, "activeConnectionCount") || strings.Contains(line, "stream is ready") {
			e.mu.Lock()
			shouldApply := !e.sbApplied
			if shouldApply {
				e.sbApplied = true
			}
			e.mu.Unlock()

			if shouldApply {
				e.wg.Add(1)
				go func() {
					defer e.wg.Done()

					runtime.EventsEmit(e.appCtx, "log", "INFO", "[SB] Запуск sing-box TUN...")

					if err := e.sbTun.Start(e.sbCfgPath); err != nil {
						msg := fmt.Sprintf("[SB] Ошибка: %v", err)
						runtime.EventsEmit(e.appCtx, "error", msg)
						runtime.EventsEmit(e.appCtx, "log", "ERROR", msg)
						e.mu.Lock()
						e.sbApplied = false
						e.mu.Unlock()
					} else {
						runtime.EventsEmit(e.appCtx, "state_changed", "running", "")
						runtime.EventsEmit(e.appCtx, "log", "INFO", "[SB] Туннель активен ✓")
						if e.onTray != nil {
							e.onTray(true, 0, 0, 0)
						}
						e.startStatsLoop()

						// Диагностика типа NAT через STUN после успешного подключения
						go func() {
							time.Sleep(2 * time.Second)
							if natRes, err := CheckNATType(); err == nil && natRes != nil {
								runtime.EventsEmit(e.appCtx, "nat_info", natRes)
								runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("[NAT] Тип NAT: %s (%s)", natRes.NATType, natRes.Details))
							}
						}()
					}
				}()
			}
		}

		level := classifyLevel(line)
		runtime.EventsEmit(e.appCtx, "log", level, line)

		lowerLineForErr := strings.ToLower(line)
		if strings.Contains(lowerLineForErr, "fatal") || strings.Contains(lowerLineForErr, "error") {
			now := time.Now()
			if now.Sub(lastErrTime) > 5*time.Second {
				runtime.EventsEmit(e.appCtx, "error", line)
				lastErrTime = now
			}
		}
	}
}
