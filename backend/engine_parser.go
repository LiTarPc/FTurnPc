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
			emitSessionLog(e.appCtx, "WARN", "[SB] Ошибка получения токена VK для потока. Ожидание автоматической повторной попытки...")
		}

		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "localhost:8765") || strings.Contains(lowerLine, "localhost:2212") ||
			strings.Contains(lowerLine, "127.0.0.1:8765") || strings.Contains(lowerLine, "127.0.0.1:2212") ||
			strings.Contains(lowerLine, "manual captcha") {
			emitSessionLog(e.appCtx, "WARN", "[SB] Требуется ввод капчи. Ожидание действий пользователя (туннель не отключается)...")
		}

		if isFreeTurnReadyLine(line) {
			e.startSingboxWhenReady()
		}

		bounded := boundedLogLine(line, 4096)
		level := classifyLevel(line)
		emitSessionLog(e.appCtx, level, bounded)
		if strings.Contains(lowerLine, "fatal") || strings.Contains(lowerLine, "error") {
			now := time.Now()
			if now.Sub(lastErrTime) > 5*time.Second {
				// Keep the dedicated UI error event for the frontend modal/toast, while
				// emitSessionLog above guarantees the same line is persisted on disk.
				runtime.EventsEmit(e.appCtx, "error", bounded)
				lastErrTime = now
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[FT] Ошибка чтения логов FreeTurn: %v", err)
	}
}

// trackFreeTurnStream keeps the UI stream counter in sync with both FreeTurn
// relay implementations. UDP mode logs [STREAM N], while TCP mode uses
// [session N].
func (e *FreeturnEngine) trackFreeTurnStream(line string) {
	streamID := freeTurnStreamID(line)
	if streamID == "" {
		return
	}

	lower := strings.ToLower(line)
	active := strings.Contains(lower, "turn allocation up") ||
		strings.Contains(lower, "relayed-address") ||
		strings.Contains(lower, "established dtls connection") ||
		strings.Contains(lower, "stream is ready") ||
		strings.Contains(lower, "] connected (active:")
	inactive := strings.Contains(lower, "turn allocation released") ||
		strings.Contains(lower, "] disconnected") ||
		strings.Contains(lower, "closed") ||
		strings.Contains(lower, "failed")

	if !active && !inactive {
		return
	}

	e.muStreams.Lock()
	defer e.muStreams.Unlock()
	if e.activeStreams == nil {
		e.activeStreams = make(map[string]bool)
	}
	if active {
		e.activeStreams[streamID] = true
	}
	if inactive {
		delete(e.activeStreams, streamID)
	}
}

func freeTurnStreamID(line string) string {
	lower := strings.ToLower(line)
	for _, prefix := range []string{"[stream ", "[session "} {
		idx := strings.Index(lower, prefix)
		if idx == -1 {
			continue
		}
		sub := line[idx+len(prefix):]
		end := strings.Index(sub, "]")
		if end == -1 {
			continue
		}
		streamID := strings.TrimSpace(sub[:end])
		if streamID != "" {
			return streamID
		}
	}
	return ""
}

var activeConnectionCountRE = regexp.MustCompile(`(?i)activeConnectionCount[^0-9]+([0-9]+)`)

// isFreeTurnReadyLine recognises stable INFO-level readiness signals from both
// FreeTurn relay modes and older client versions.
//
// "Established DTLS connection" is currently a Debugf line in FreeTurn UDP
// mode, so a normal production client never prints it unless -debug is enabled.
// "TURN allocation up" is the first reliable INFO-level UDP signal. The local
// UDP listener is already bound by then, so sing-box may safely start sending
// datagrams while DTLS finishes establishing.
func isFreeTurnReadyLine(line string) bool {
	lower := strings.ToLower(line)

	// UDP mode: WireGuard / Hysteria2 / TUIC.
	if strings.Contains(lower, "turn allocation up") {
		return true
	}

	// TCP mode: VLESS / VMess / Trojan / Shadowsocks. Wait for a real pooled
	// session rather than merely for the local TCP listener to open.
	if strings.Contains(lower, "tcp mode: pool serving traffic") ||
		(strings.Contains(lower, "[session ") && strings.Contains(lower, "] connected (active:")) {
		return true
	}

	// Compatibility with older/debug FreeTurn builds.
	if strings.Contains(lower, "established dtls connection") || strings.Contains(lower, "stream is ready") {
		return true
	}
	match := activeConnectionCountRE.FindStringSubmatch(line)
	if len(match) != 2 {
		return false
	}
	count, err := strconv.Atoi(match[1])
	return err == nil && count >= 1
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

	// This INFO line makes the transport -> TUN hand-off visible in the UI and
	// makes future startup failures much easier to diagnose from user logs.
	emitSessionLog(e.appCtx, "INFO", "[FT] Транспорт FreeTurn готов; запускаем sing-box...")

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		emitSessionLog(e.appCtx, "INFO", "[SB] Запуск sing-box TUN...")

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
			emitSessionLog(e.appCtx, "ERROR", msg)
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
		emitSessionLog(e.appCtx, "INFO", "[SB] Туннель активен ✓")
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
		emitSessionLog(e.appCtx, "INFO", fmt.Sprintf("[NAT] Тип NAT: %s (%s)", natRes.NATType, natRes.Details))
	}
}
