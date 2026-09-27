package backend

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	turnRouteEnsureRE = regexp.MustCompile(`(?i)Ensuring route to ([0-9.]+)/32(?:\s|$)`)
	turnRouteFailedRE = regexp.MustCompile(`(?i)failed to add route to ([0-9.]+)(?::|\s|$)`)
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
		if uiManagesTurnRoutes() {
			e.trackManagedTurnServer(line)
		} else {
			e.trackTurnRoute(line)
		}

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
			e.mu.Lock()
			e.ftReady = true
			e.mu.Unlock()
			e.startSingboxWhenReady()
		}
		if !safeFreeTurnLogLine(line) {
			continue
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

// -debug is needed for DTLS readiness and TURN candidate discovery, but the
// core also prints captcha request bodies and browser cookies at that level.
// Parse those lines internally, then keep them out of persisted/UI logs.
func safeFreeTurnLogLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"session_token", "access_token", "client_secret", "cookie =", "cookie:", "authorization =", "ft_admin_session"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	if strings.Contains(lower, "[captcha proxy]") {
		return false
	}
	if strings.Contains(lower, "[captcha]") {
		for _, marker := range []string{"solving captcha (", "solving vk smart captcha automatically", "solver succeeded", "triggering manual captcha", "got token from browser"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
		return false
	}
	return true
}

// FreeTurn announces TURN candidates before, or at latest when, allocation is
// complete. Install a physical /32 before sing-box changes the default route.
func (e *FreeturnEngine) trackManagedTurnServer(line string) {
	ip := freeTurnServerIP(line)
	if ip == "" {
		return
	}
	e.turnRoutesMu.Lock()
	if _, seen := e.turnRoutes[ip]; seen {
		e.turnRoutesMu.Unlock()
		return
	}
	if time.Since(e.turnRouteFailures[ip]) < 10*time.Second {
		e.turnRoutesMu.Unlock()
		return
	}
	route, err := addManagedTurnRoute(ip)
	if route != nil {
		if e.managedTurnRoutes == nil {
			e.managedTurnRoutes = make(map[string]managedTurnRoute)
		}
		e.managedTurnRoutes[ip] = *route
	}
	if err == nil {
		delete(e.turnRouteFailures, ip)
		if e.turnRoutes == nil {
			e.turnRoutes = make(map[string]struct{})
		}
		e.turnRoutes[ip] = struct{}{}
	} else {
		if e.turnRouteFailures == nil {
			e.turnRouteFailures = make(map[string]time.Time)
		}
		e.turnRouteFailures[ip] = time.Now()
	}
	e.turnRoutesMu.Unlock()
	if err != nil {
		emitSessionLog(e.appCtx, "WARN", fmt.Sprintf("[FT] Маршрут к TURN %s не установлен: %v", ip, err))
		return
	}
	if route != nil {
		emitSessionLog(e.appCtx, "INFO", fmt.Sprintf("[FT] TURN %s/32 через %s (interface %d)", ip, route.gateway, route.ifIndex))
	}
	e.mu.Lock()
	ready := e.ftReady
	e.mu.Unlock()
	if ready {
		e.startSingboxWhenReady()
	}
}

func freeTurnServerIP(line string) string {
	var value string
	if i := strings.Index(line, "server="); i >= 0 && strings.Contains(strings.ToLower(line), "turn allocation up") {
		value = line[i+len("server="):]
	} else if i := strings.Index(line, "TURN server IP:"); i >= 0 {
		value = line[i+len("TURN server IP:"):]
	} else if i := strings.Index(strings.ToLower(line), "selected turn:"); i >= 0 {
		value = line[i+len("selected turn:"):]
	} else if strings.Contains(line, "Resolved TURN server") || strings.Contains(line, "Resolved STUN server") {
		if _, after, ok := strings.Cut(line, " to "); ok {
			value = after
		}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value, _, _ = strings.Cut(value, ":")
	value = strings.Trim(value, " )\"'\r\n\t")
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() {
		return ""
	}
	return ip.String()
}

// trackTurnRoute mirrors routes that FreeTurn says it intends to manage. We
// keep a route in this fallback set until process exit, even when FreeTurn logs
// its normal "Removing route" line: that line is emitted before route deletion,
// so deletion can still fail. cleanupTrackedTurnRoutes verifies whether a route
// still exists before attempting the final cleanup.
func (e *FreeturnEngine) trackTurnRoute(line string) {
	if match := turnRouteEnsureRE.FindStringSubmatch(line); len(match) == 2 {
		if ip := net.ParseIP(match[1]); ip != nil && ip.To4() != nil {
			e.turnRoutesMu.Lock()
			if e.turnRoutes == nil {
				e.turnRoutes = make(map[string]struct{})
			}
			e.turnRoutes[ip.String()] = struct{}{}
			e.turnRoutesMu.Unlock()
		}
		return
	}

	// If route add failed (for example because an identical route already
	// existed), do not let fallback cleanup delete a route we did not create.
	if match := turnRouteFailedRE.FindStringSubmatch(line); len(match) == 2 {
		e.forgetTurnRoute(match[1])
	}
}

func (e *FreeturnEngine) forgetTurnRoute(rawIP string) {
	ip := net.ParseIP(rawIP)
	if ip == nil || ip.To4() == nil {
		return
	}
	e.turnRoutesMu.Lock()
	delete(e.turnRoutes, ip.String())
	e.turnRoutesMu.Unlock()
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
		strings.Contains(lower, "closed dtls connection") ||
		strings.Contains(lower, "] closed")

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

// isFreeTurnReadyLine recognises established transport readiness signals from
// both FreeTurn relay modes and older client versions.
//
// "Established DTLS connection" is currently a Debugf line in FreeTurn UDP
// mode, so a normal production client never prints it unless -debug is enabled.
// "TURN allocation up" only means the relay allocated an address. It can be
// followed by DTLS or TURN failure, so wait for the debug-level DTLS handshake
// or the explicit stream-ready signal before starting sing-box.
func isFreeTurnReadyLine(line string) bool {
	lower := strings.ToLower(line)

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
	if !e.ftReady || e.sbApplied || e.sbStarting || e.sessionClosing || e.userStopped || e.cmd == nil {
		e.mu.Unlock()
		return
	}
	if uiManagesTurnRoutes() {
		e.turnRoutesMu.Lock()
		hasTurnRoute := len(e.turnRoutes) > 0
		e.turnRoutesMu.Unlock()
		if !hasTurnRoute {
			warn := !e.routePendingWarned
			e.routePendingWarned = true
			e.mu.Unlock()
			if warn {
				emitSessionLog(e.appCtx, "WARN", "[FT] DTLS готов, но маршрут к TURN ещё не установлен; ожидаем IP TURN-сервера")
			}
			return
		}
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
		if uiManagesTurnRoutes() {
			e.turnRoutesMu.Lock()
			ips := make([]string, 0, len(e.turnRoutes))
			for ip := range e.turnRoutes {
				ips = append(ips, ip)
			}
			if e.protectedPeerIP != "" {
				ips = append(ips, e.protectedPeerIP)
			}
			e.turnRoutesMu.Unlock()
			if err := verifyManagedTurnRoutes(ips); err != nil {
				msg := fmt.Sprintf("[FT] Маршрут TURN изменился после запуска sing-box: %v", err)
				emitSessionLog(e.appCtx, "ERROR", msg)
				runtime.EventsEmit(e.appCtx, "error", msg)
				e.sbTun.Stop()
				e.fail(err)
				return
			}
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

		// Establish the traffic baseline before consumers are told that the
		// tunnel is ready. Otherwise traffic started immediately in response to
		// the running event can slip in ahead of the baseline.
		e.startStatsLoop()
		runtime.EventsEmit(e.appCtx, "state_changed", "running", "")
		emitSessionLog(e.appCtx, "INFO", "[SB] Туннель активен ✓")
		if e.onTray != nil {
			e.onTray(true, 0, 0, 0)
		}
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
