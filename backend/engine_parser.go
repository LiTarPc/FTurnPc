package backend

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// addTurnIP валидирует и добавляет обнаруженный IP TURN-сервера в список исключений туннеля.
func (e *FreeturnEngine) addTurnIP(ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return
	}

	e.muIPs.Lock()
	if e.turnIPs == nil {
		e.turnIPs = make(map[string]bool)
	}
	isNew := !e.turnIPs[ip]
	e.turnIPs[ip] = true
	e.muIPs.Unlock()

	if isNew {
		runtime.EventsEmit(e.appCtx, "log", "DEBUG", fmt.Sprintf("[WG] Обнаружен новый TURN IP: %s", ip))
		// Для loopback (127.x.x.x, localhost) маршрут через шлюз не создается
		if parsedIP.IsLoopback() {
			return
		}

		e.mu.Lock()
		applied := e.wgApplied
		e.mu.Unlock()
		if applied {
			if err := AddBypassRoute(ip); err != nil {
				runtime.EventsEmit(e.appCtx, "log", "WARNING", fmt.Sprintf("[WG] Ошибка динамического байпаса TURN %s: %v", ip, err))
			} else {
				runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("[WG] Добавлен динамический маршрут для TURN: %s", ip))
			}
		}
	}
}

// parseLogs построчно читает вывод FreeTurn, отслеживает состояние потоков,
// извлекает адреса TURN/STUN серверов и детектирует системные события.
func (e *FreeturnEngine) parseLogs(r io.Reader, wgConfig string, bypassRu bool, customMTU int) {
	defer e.wg.Done()
	scanner := bufio.NewScanner(r)

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

		// 2. Извлечение IP STUN / TURN серверов
		if strings.Contains(line, "Resolved STUN server") || strings.Contains(line, "Resolved TURN server") {
			parts := strings.Split(line, " to ")
			if len(parts) == 2 {
				ipPort := strings.TrimSpace(parts[1])
				ip, _, _ := strings.Cut(ipPort, ":")
				e.addTurnIP(ip)
			}
		}
		if strings.Contains(line, "TURN server IP:") {
			parts := strings.Split(line, "TURN server IP:")
			if len(parts) == 2 {
				ip := strings.TrimSpace(parts[1])
				e.addTurnIP(ip)
			}
		}
		if strings.Contains(line, "selected turn:") {
			parts := strings.Split(line, "selected turn:")
			if len(parts) == 2 {
				ipPort := strings.TrimSpace(parts[1])
				host, _, _ := strings.Cut(ipPort, ":")
				e.addTurnIP(host)
			}
		}
		if strings.Contains(line, "server=") {
			idx := strings.Index(line, "server=")
			if idx != -1 {
				sub := line[idx+7:]
				end := strings.IndexAny(sub, " )\"'\r\n\t")
				var addr string
				if end != -1 {
					addr = sub[:end]
				} else {
					addr = sub
				}
				ip, _, _ := strings.Cut(addr, ":")
				e.addTurnIP(ip)
			}
		}
		if idx := strings.Index(line, "turn:"); idx != -1 {
			sub := line[idx+5:]
			end := strings.IndexAny(sub, " :\"'\r\n\t")
			if end != -1 {
				ip := sub[:end]
				e.addTurnIP(ip)
			}
		}

		// 3. Блокировка VK DNS авторизации
		if strings.Contains(line, "all VK credentials failed") {
			runtime.EventsEmit(e.appCtx, "log", "ERROR", "[WG] Обнаружена блокировка DNS для VK Auth. Принудительный перезапуск туннеля...")
			e.mu.Lock()
			cmd := e.cmd
			e.mu.Unlock()
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}

		// 4. Детекция запроса ручного ввода капчи (поддержка портов 8765 и 2212)
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "localhost:8765") || strings.Contains(lowerLine, "localhost:2212") ||
			strings.Contains(lowerLine, "127.0.0.1:8765") || strings.Contains(lowerLine, "127.0.0.1:2212") ||
			strings.Contains(lowerLine, "manual captcha") {
			runtime.EventsEmit(e.appCtx, "log", "WARNING", "[WG] Требуется ввод капчи. Ожидание действий пользователя (туннель не отключается)...")
		}

		// 5. Поднятие WireGuard при готовности DTLS сессии
		if strings.Contains(line, "Established DTLS connection") || strings.Contains(line, "activeConnectionCount") || strings.Contains(line, "stream is ready") {
			e.mu.Lock()
			shouldApply := !e.wgApplied
			if shouldApply {
				e.wgApplied = true
			}
			e.mu.Unlock()

			if shouldApply {
				go func() {
					e.muIPs.Lock()
					ips := make([]string, 0, len(e.turnIPs))
					for ip := range e.turnIPs {
						ips = append(ips, ip)
					}
					e.muIPs.Unlock()

					runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("[WG] Применение конфига (стартовый байпас: %v)...", ips))

					if err := applyWGConfig(wgConfig, ips, bypassRu, customMTU); err != nil {
						msg := fmt.Sprintf("[WG] Ошибка применения конфига: %v", err)
						runtime.EventsEmit(e.appCtx, "error", msg)
						runtime.EventsEmit(e.appCtx, "log", "ERROR", msg)
						e.mu.Lock()
						e.wgApplied = false
						e.mu.Unlock()
					} else {
						runtime.EventsEmit(e.appCtx, "state_changed", "running", "")
						runtime.EventsEmit(e.appCtx, "log", "INFO", "[WG] Конфиг применён, туннель активен ✓")
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

		if strings.Contains(line, "fatal") || strings.Contains(line, "error") {
			runtime.EventsEmit(e.appCtx, "error", line)
		}
	}
}
