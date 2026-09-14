package backend

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// parseLogs reads FreeTurn output, tracks streams and starts sing-box after the
// transport has actually established a usable session.
func (e *FreeturnEngine) parseLogs(r io.Reader) {
	defer e.wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var lastErrTime time.Time

	for scanner.Scan() {
		line := scanner.Text()
		e.trackFreeTurnStream(line)

		if strings.Contains(line, "all VK credentials failed") {
			runtime.EventsEmit(e.appCtx, "log", "WARN", "[SB] Ошибка получения токена VK для потока. Ожидание автоматической повторной попытки...")
		}

		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "localhost:8765") || strings.Contains(lowerLine, "localhost:2212") ||
			strings.Contains(lowerLine, "127.0.0.1:8765") || strings.Contains(lowerLine, "127.0.0.1:2212") ||
			strings.Contains(lowerLine, "manual captcha") {
			runtime.EventsEmit(e.appCtx, "log", "WARNING", "[SB] Требуется ввод капчи. Ожидание действий пользователя (туннель не отключается)...")
		}

		if isFreeTurnReadyLine(line) {
			e.startSingboxWhenReady()
		}

		level := classifyLevel(line)
		runtime.EventsEmit(e.appCtx, "log", level, boundedLogLine(line, 4096))
		if strings.Contains(lowerLine, "fatal") || strings.Contains(lowerLine, "error") {
			now := time.Now()
			if now.Sub(lastErrTime) > 5*time.Second {
				runtime.EventsEmit(e.appCtx, "error", boundedLogLine(line, 4096))
				lastErrTime = now
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[FT] Ошибка чтения логов FreeTurn: %v", err)
	}
}

func (e *FreeturnEngine) trackFreeTurnStream(line string) {
	idx := strings.Index(line, "[STREAM ")
	if idx == -1 {
		return
	}
	sub := line[idx+8:]
	end := strings.Index(sub, "]")
	if end == -1 {
		return
	}
	streamID := strings.TrimSpace(sub[:end])
	if streamID == "" {
		return
	}
	e.muStreams.Lock()
	defer e.muStreams.Unlock()
	if e.activeStreams == nil {
		e.activeStreams = make(map[string]bool)
	}
	if strings.Contains(line, "relayed-address") || strings.Contains(line, "Established") || strings.Contains(line, "stream is ready") {
		e.activeStreams[streamID] = true
	}
	if strings.Contains(line, "closed") || strings.Contains(line, "failed") {
		delete(e.activeStreams, streamID)
	}
}

var activeConnectionCountRE = regexp.MustCompile(`(?i)activeConnectionCount[^0-9]+([0-9]+)`)

func isFreeTurnReadyLine(line string) bool {
	if strings.Contains(line, "Established DTLS connection") || strings.Contains(line, "stream is ready") {
		return true
	}
	match := activeConnectionCountRE.FindStringSubmatch(line)
	if len(match) != 2 {
		return false
	}
	count, err := strconv.Atoi(match[1])
	return err == nil && count >= 0
}

func (e *FreeturnEngine) startSingboxWhenReady() {
	e.mu.Lock()
	if e.sbApplied || e.sbStarting || e.sessionClosing || e.userStopped || e.cmd == nil {
		e.mu.Unlock()
		return
	}
	e.sbStarting = true
	cfgPath := e.sbCfgPath
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		runtime.EventsEmit(e.appCtx, "log", "INFO", "[SB] Запуск sing-box TUN...")

		if err := e.sbTun.Start(cfgPath); err != nil {
			e.mu.Lock()
			e.sbStarting = false
			e.sbApplied = false
			closing := e.sessionClosing || e.userStopped
			e.mu.Unlock()
			if closing {
				return
			}
			msg := fmt.Sprintf("[SB] Ошибка запуска: %v", err)
			runtime.EventsEmit(e.appCtx, "error", msg)
			runtime.EventsEmit(e.appCtx, "log", "ERROR", msg)
			e.fail(fmt.Errorf("sing-box startup failed: %w", err))
			return
		}

		e.mu.Lock()
		if e.sessionClosing || e.userStopped || e.cmd == nil {
			e.sbStarting = false
			e.mu.Unlock()
			e.sbTun.Stop()
			return
		}
		e.sbStarting = false
		e.sbApplied = true
		e.mu.Unlock()

		runtime.EventsEmit(e.appCtx, "state_changed", "running", "")
		runtime.EventsEmit(e.appCtx, "log", "INFO", "[SB] Туннель активен ✓")
		if e.onTray != nil {
			e.onTray(true, 0, 0, 0)
		}
		e.startStatsLoop()
		go e.emitNATInfoAfterDelay()
	}()
}

func (e *FreeturnEngine) emitNATInfoAfterDelay() {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-e.appCtx.Done():
		return
	}

	e.mu.Lock()
	active := !e.sessionClosing && !e.userStopped && e.sbApplied && e.cmd != nil
	e.mu.Unlock()
	if !active {
		return
	}
	if natRes, err := CheckNATType(); err == nil && natRes != nil {
		runtime.EventsEmit(e.appCtx, "nat_info", natRes)
		runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("[NAT] Тип NAT: %s (%s)", natRes.NATType, natRes.Details))
	}
}
