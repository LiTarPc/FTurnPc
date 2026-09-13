package backend

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// FreeturnEngine управляет дочерним процессом freeturnclient и sing-box.
type FreeturnEngine struct {
	appCtx context.Context
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
	wg     sync.WaitGroup

	onTray            func(connected bool, rx, tx int64, workers int32)
	onUnexpectedExit  func(err error)
	userStopped       bool
	sbTun             *SingboxTun // менеджер sing-box процесса
	sbCfgPath         string
	sbApplied         bool
	configuredStreams int
	muStreams         sync.Mutex
	activeStreams     map[string]bool
	statsStop         chan struct{}
	exitChan          chan struct{}
}

func NewFreeturnEngine(ctx context.Context, onTray func(bool, int64, int64, int32), onUnexpectedExit func(err error)) *FreeturnEngine {
	return &FreeturnEngine{
		appCtx:           ctx,
		onTray:           onTray,
		onUnexpectedExit: onUnexpectedExit,
		sbTun:            &SingboxTun{appCtx: ctx},
	}
}

func (e *FreeturnEngine) Start(p ConnectParams, prof *ProfileData) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd != nil {
		return fmt.Errorf("already running")
	}

	e.sbApplied = false

	e.muStreams.Lock()
	e.activeStreams = make(map[string]bool)
	e.muStreams.Unlock()
	e.statsStop = nil

	// ── Генерация sing-box конфига заранее ──────────────────────────────────
	cfgBytes, err := BuildSingboxConfig(prof, p)
	if err != nil {
		return fmt.Errorf("ошибка генерации sing-box конфига: %w", err)
	}
	e.sbCfgPath = filepath.Join(singboxDir(), "config.json")
	if err := os.WriteFile(e.sbCfgPath, cfgBytes, 0o600); err != nil {
		return fmt.Errorf("не удалось записать конфиг: %w", err)
	}
	log.Printf("[SB] Конфиг сохранён: %s", e.sbCfgPath)

	// ── Подготовка freeturnclient ───────────────────────────────────────────
	peerIP := prof.PeerAddr
	if host, _, err := net.SplitHostPort(prof.PeerAddr); err == nil {
		peerIP = host
	}
	peerIP = strings.TrimSpace(peerIP)
	if peerIP == "localhost" {
		peerIP = "127.0.0.1"
	}

	exePath := getFreeturnPath()
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("freeturnclient не найден по пути: %s", exePath)
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
		if prof.Power > 0 {
			workers = prof.Power
		} else {
			workers = 10
		}
	}
	e.configuredStreams = workers
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
	args = append(args, "-streams-per-cred", fmt.Sprintf("%d", streams))

	coreVer := GetCoreVersion()
	isLegacy := strings.HasPrefix(coreVer, "v1.") || strings.HasPrefix(coreVer, "v2.") || strings.HasPrefix(coreVer, "Бинарный")
	if !strings.HasPrefix(coreVer, "v") && (strings.HasPrefix(coreVer, "1.") || strings.HasPrefix(coreVer, "2.")) {
		isLegacy = true
	}
	if isLegacy {
		args = append(args, "-mode", "udp")
	}

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
	e.exitChan = make(chan struct{})
	e.cmd = exec.CommandContext(ctx, exePath, args...)

	// Скрытие окна консоли на Windows
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

	safeArgs := make([]string, len(args))
	copy(safeArgs, args)
	for i, arg := range safeArgs {
		if arg == "-obf-key" || arg == "-client-id" || arg == "-links" {
			if i+1 < len(safeArgs) {
				safeArgs[i+1] = "***"
			}
		}
	}
	runtime.EventsEmit(e.appCtx, "log", "DEBUG", fmt.Sprintf("Launching freeturn: %s %v", exePath, safeArgs))

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
	go e.parseLogs(stdout)
	go e.parseLogs(stderr)

	go func() {
		defer close(e.exitChan)
		err := e.cmd.Wait()
		e.mu.Lock()
		stopped := e.userStopped
		e.stopStatsLoopLocked()
		e.mu.Unlock()
		e.wg.Wait()

		// Останавливаем sing-box при завершении freeturnclient
		e.sbTun.Stop()

		runtime.EventsEmit(e.appCtx, "log", "INFO", fmt.Sprintf("Сессия FreeTurn завершена (err: %v)", err))
		if stopped {
			runtime.EventsEmit(e.appCtx, "state_changed", "disconnected", "")
			if e.onTray != nil {
				e.onTray(false, 0, 0, 0)
			}
		} else {
			runtime.EventsEmit(e.appCtx, "state_changed", "connecting", "")
			if e.onTray != nil {
				e.onTray(false, 0, 0, 0)
			}
		}

		e.mu.Lock()
		e.cmd = nil
		e.cancel = nil
		e.mu.Unlock()

		if !stopped && e.onUnexpectedExit != nil {
			e.onUnexpectedExit(err)
		}
	}()

	return nil
}

func (e *FreeturnEngine) Stop() {
	e.mu.Lock()
	e.userStopped = true
	cancel := e.cancel
	cmd := e.cmd
	exitChan := e.exitChan
	e.mu.Unlock()

	// Сначала TUN, потом транспорт
	e.sbTun.Stop()

	e.mu.Lock()
	e.stopStatsLoopLocked()
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if exitChan != nil {
		<-exitChan
	}
}

func (e *FreeturnEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cmd != nil
}
