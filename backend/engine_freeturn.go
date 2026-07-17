package backend

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type FreeturnEngine struct {
	appCtx context.Context
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
	wg     sync.WaitGroup

	onTray  func(connected bool, rx, tx int64, workers int32)
	muIPs   sync.Mutex
	turnIPs map[string]bool
}

func NewFreeturnEngine(ctx context.Context, onTray func(bool, int64, int64, int32)) *FreeturnEngine {
	return &FreeturnEngine{
		appCtx:  ctx,
		onTray:  onTray,
		turnIPs: make(map[string]bool),
	}
}

func (e *FreeturnEngine) addTurnIP(ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	if strings.Contains(ip, ".") || strings.Contains(ip, ":") {
		e.muIPs.Lock()
		if e.turnIPs == nil {
			e.turnIPs = make(map[string]bool)
		}
		e.turnIPs[ip] = true
		e.muIPs.Unlock()
	}
}

func (e *FreeturnEngine) Start(p ConnectParams, prof *ProfileData) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd != nil {
		return fmt.Errorf("already running")
	}

	e.muIPs.Lock()
	e.turnIPs = make(map[string]bool)
	e.muIPs.Unlock()

	peerIP, _, _ := strings.Cut(prof.PeerAddr, ":")
	e.addTurnIP(peerIP)

	exePath := getFreeturnPath()
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("freeturnclient.exe не найден по пути: %s", exePath)
	}

	args := []string{
		"-listen", "127.0.0.1:9000",
		"-peer", prof.PeerAddr,
	}
	if prof.Links != "" {
		args = append(args, "-links", prof.Links)
	}
	
	workers := p.Workers
	if workers <= 0 {
		workers = 10
	}
	args = append(args, "-n", fmt.Sprintf("%d", workers))
	
	transport := prof.Transport
	if transport == "" {
		transport = "tcp"
	}
	args = append(args, "-transport", transport)
	
	streams := prof.StreamsPerCred
	if streams <= 0 {
		streams = 5
	}
	args = append(args, "-streams-per-cred", fmt.Sprintf("%d", streams), "-mode", "udp")
	
	if prof.Obf != "" {
		args = append(args, "-obf-profile", prof.Obf)
	}
	if prof.Key != "" {
		args = append(args, "-obf-key", prof.Key)
	}
	if prof.Cid != "" {
		args = append(args, "-client-id", prof.Cid)
	}
	args = append(args, "-debug")

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.cmd = exec.CommandContext(ctx, exePath, args...)
	
	// Hide window on Windows
	hideWindow(e.cmd)

	stdout, err := e.cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %v", err)
	}
	stderr, err := e.cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stderr pipe: %v", err)
	}

	runtime.EventsEmit(e.appCtx, "log", "DEBUG", fmt.Sprintf("Launching freeturn: %s %v", exePath, args))

	if err := e.cmd.Start(); err != nil {
		cancel()
		e.cmd = nil
		return fmt.Errorf("failed to start freeturn: %v", err)
	}

	runtime.EventsEmit(e.appCtx, "state_changed", "connecting", "")
	if e.onTray != nil {
		e.onTray(false, 0, 0, 0)
	}

	e.wg.Add(2)
	go e.parseLogs(stdout, prof.WGConfig)
	go e.parseLogs(stderr, prof.WGConfig)

	go func() {
		err := e.cmd.Wait()
		e.wg.Wait()
		teardownWG()

		runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("Сессия FreeTurn завершена (err: %v)", err))
		runtime.EventsEmit(e.appCtx, "state_changed", "disconnected", "")
		if e.onTray != nil {
			e.onTray(false, 0, 0, 0)
		}
		e.mu.Lock()
		e.cmd = nil
		e.cancel = nil
		e.mu.Unlock()
	}()

	return nil
}

func (e *FreeturnEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
}

func (e *FreeturnEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cmd != nil
}

func (e *FreeturnEngine) parseLogs(r interface{ Read([]byte) (int, error) }, wgConfig string) {
	defer e.wg.Done()
	scanner := bufio.NewScanner(r)
	wgApplied := false

	for scanner.Scan() {
		line := scanner.Text()
		
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
				end := strings.IndexAny(sub, " )\"'")
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
		
		// FreeTurn client emits "Established DTLS connection" when a stream is ready
		if !wgApplied && (strings.Contains(line, "Established DTLS connection") || strings.Contains(line, "activeConnectionCount") || strings.Contains(line, "stream is ready")) {
			wgApplied = true
			go func() {
				runtime.EventsEmit(e.appCtx, "log", "INFO", "[WG] Ожидание 4 сек, чтобы все потоки успели подключиться...")
				time.Sleep(4 * time.Second)
				
				e.muIPs.Lock()
				ips := make([]string, 0, len(e.turnIPs))
				for ip := range e.turnIPs {
					ips = append(ips, ip)
				}
				e.muIPs.Unlock()
				
				runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("[WG] Применение конфига (исключения: %v)...", ips))
				
				if err := applyWGConfig(wgConfig, ips); err != nil {
					msg := fmt.Sprintf("[WG] Ошибка применения конфига: %v", err)
					runtime.EventsEmit(e.appCtx, "error", msg)
					runtime.EventsEmit(e.appCtx, "log", "ERROR", msg)
				} else {
					runtime.EventsEmit(e.appCtx, "state_changed", "running", "")
					runtime.EventsEmit(e.appCtx, "log", "INFO", "[WG] Конфиг применён, туннель активен ✓")
					if e.onTray != nil {
						e.onTray(true, 0, 0, 0)
					}
				}
			}()
		}
		
		level := classifyLevel(line)
		runtime.EventsEmit(e.appCtx, "log", level, line)
		
		if strings.Contains(line, "fatal") || strings.Contains(line, "error") {
			runtime.EventsEmit(e.appCtx, "error", line)
		}
	}
}

func getFreeturnPath() string {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	path := filepath.Join(dir, "assets", "freeturn", "freeturnclient.exe")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join(dir, "freeturnclient.exe")
}
