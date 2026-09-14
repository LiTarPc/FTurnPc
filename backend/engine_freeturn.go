package backend

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// FreeturnEngine manages freeturnclient and the dependent sing-box TUN process.
type FreeturnEngine struct {
	appCtx context.Context
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
	wg     sync.WaitGroup

	onTray           func(connected bool, rx, tx int64, workers int32)
	onUnexpectedExit func(err error)
	userStopped      bool
	sessionClosing   bool
	failureErr       error

	sbTun      *SingboxTun
	sbCfgPath  string
	sbApplied  bool
	sbStarting bool

	configuredStreams int
	muStreams         sync.Mutex
	activeStreams     map[string]bool
	statsStop         chan struct{}
	exitChan          chan struct{}
}

func NewFreeturnEngine(ctx context.Context, onTray func(bool, int64, int64, int32), onUnexpectedExit func(err error)) *FreeturnEngine {
	e := &FreeturnEngine{
		appCtx:           ctx,
		onTray:           onTray,
		onUnexpectedExit: onUnexpectedExit,
	}
	e.sbTun = &SingboxTun{
		appCtx: ctx,
		onUnexpectedExit: func(err error) {
			e.fail(fmt.Errorf("sing-box неожиданно завершился: %w", err))
		},
	}
	return e
}

func (e *FreeturnEngine) Start(p ConnectParams, prof *ProfileData) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd != nil {
		return fmt.Errorf("already running")
	}

	e.userStopped = false
	e.sessionClosing = false
	e.failureErr = nil
	e.sbApplied = false
	e.sbStarting = false
	e.statsStop = nil
	e.muStreams.Lock()
	e.activeStreams = make(map[string]bool)
	e.muStreams.Unlock()

	mode, err := ResolveFreeTurnMode(prof)
	if err != nil {
		return fmt.Errorf("FreeTurn mode: %w", err)
	}
	transport := prof.Transport
	if transport == "" {
		transport = "tcp"
	}
	emitSessionLog(e.appCtx, "INFO", fmt.Sprintf("[FT] relay mode=%s, TURN transport=%s, local=%s:9000", mode, transport, freeTurnHost))

	// Generate and validate sing-box before spending time establishing FreeTurn.
	cfgBytes, err := BuildSingboxConfig(prof, p)
	if err != nil {
		return fmt.Errorf("ошибка генерации sing-box конфига: %w", err)
	}
	// The real proxy hop is always localhost:9000. Keep that hop completely
	// outside the TUN route and explicitly bind its dialer to loopback so
	// route.auto_detect_interface cannot force the socket onto the physical NIC.
	cfgBytes, err = HardenSingboxLoopbackConfig(cfgBytes)
	if err != nil {
		return fmt.Errorf("ошибка настройки loopback bypass sing-box: %w", err)
	}
	emitSessionLog(e.appCtx, "INFO", "[SB] loopback bypass: exclude 127.0.0.0/8, bind proxy hop to 127.0.0.1")

	cfgPath, err := writeSingboxSessionConfig(cfgBytes)
	if err != nil {
		return err
	}
	e.sbCfgPath = cfgPath
	cleanupConfigOnError := true
	defer func() {
		if cleanupConfigOnError {
			e.cleanupSingboxConfigLocked()
		}
	}()

	sbPath := getSingboxPath()
	if sbPath == "" {
		return fmt.Errorf("ядро sing-box не найдено")
	}
	if err := validateSingboxVersion(sbPath); err != nil {
		return fmt.Errorf("sing-box version: %w", err)
	}
	if err := singboxCheck(sbPath, e.sbCfgPath); err != nil {
		return fmt.Errorf("невалидный sing-box конфиг: %w", err)
	}
	emitSessionLog(e.appCtx, "INFO", "[SB] sing-box check OK; ожидаем готовность FreeTurn")

	exePath := getFreeturnPath()
	if st, err := os.Stat(exePath); err != nil || st.IsDir() {
		if err == nil {
			err = fmt.Errorf("path is a directory")
		}
		return fmt.Errorf("freeturnclient недоступен по пути %s: %w", exePath, err)
	}

	args := buildFreeTurnArgs(p, prof, mode)
	ctx := e.appCtx
	if ctx == nil {
		ctx = context.Background()
	}
	procCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.exitChan = make(chan struct{})
	cmd := exec.CommandContext(procCtx, exePath, args...)
	prepareProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		e.cancel = nil
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		e.cancel = nil
		return fmt.Errorf("stderr pipe: %w", err)
	}

	emitSessionLog(e.appCtx, "DEBUG", fmt.Sprintf("Launching freeturn: %s %v", exePath, redactFreeTurnArgs(args)))
	if err := cmd.Start(); err != nil {
		cancel()
		e.cancel = nil
		return fmt.Errorf("failed to start freeturn: %w", err)
	}
	attachToJob(cmd)
	e.cmd = cmd
	cleanupConfigOnError = false

	runtime.EventsEmit(e.appCtx, "state_changed", "connecting", "")
	if e.onTray != nil {
		e.onTray(false, 0, 0, 0)
	}

	e.wg.Add(2)
	go e.parseLogs(stdout)
	go e.parseLogs(stderr)
	go e.waitFreeTurn(cmd, e.exitChan)
	return nil
}

func buildFreeTurnArgs(p ConnectParams, prof *ProfileData, mode string) []string {
	args := []string{
		"-listen", freeTurnHost + ":9000",
		"-peer", prof.PeerAddr,
		"-mode", mode,
	}
	if prof.Links != "" {
		args = append(args, "-links", prof.Links)
	}

	workers := p.Workers
	if workers <= 0 {
		workers = prof.Power
	}
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
	args = append(args, "-streams-per-cred", fmt.Sprintf("%d", streams))
	if prof.Obf != "" {
		args = append(args, "-obf-profile", prof.Obf)
	}
	if prof.Key != "" {
		args = append(args, "-obf-key", prof.Key)
	}
	if prof.Cid != "" {
		args = append(args, "-client-id", prof.Cid)
	}
	return args
}

func redactFreeTurnArgs(args []string) []string {
	redacted := append([]string(nil), args...)
	for i, arg := range redacted {
		switch arg {
		case "-obf-key", "-client-id", "-links":
			if i+1 < len(redacted) {
				redacted[i+1] = "***"
			}
	}
	return redacted
}

func writeSingboxSessionConfig(data []byte) (string, error) {
	dir := singboxDir()
	f, err := os.CreateTemp(dir, "config-*.json")
	if err != nil {
		return "", fmt.Errorf("не удалось создать временный sing-box config: %w", err)
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return "", fmt.Errorf("chmod sing-box config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("write sing-box config: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync sing-box config: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close sing-box config: %w", err)
	}
	ok = true
	log.Printf("[SB] Временный конфиг создан: %s", path)
	return path, nil
}

func (e *FreeturnEngine) waitFreeTurn(cmd *exec.Cmd, exitChan chan struct{}) {
	defer close(exitChan)
	err := cmd.Wait()

	e.mu.Lock()
	if e.cmd != cmd {
		e.mu.Unlock()
		return
	}
	e.sessionClosing = true
	stopped := e.userStopped
	failureErr := e.failureErr
	e.stopStatsLoopLocked()
	e.mu.Unlock()

	// Prevent a parser goroutine from starting TUN while transport is already dead.
	e.sbTun.Stop()
	e.wg.Wait()

	emitSessionLog(e.appCtx, "INFO", fmt.Sprintf("Сессия FreeTurn завершена (err: %v)", err))
	if stopped {
		runtime.EventsEmit(e.appCtx, "state_changed", "disconnected", "")
	} else {
		runtime.EventsEmit(e.appCtx, "state_changed", "connecting", "")
	}
	if e.onTray != nil {
		e.onTray(false, 0, 0, 0)
	}

	e.mu.Lock()
	e.cmd = nil
	e.cancel = nil
	e.sbApplied = false
	e.sbStarting = false
	e.cleanupSingboxConfigLocked()
	e.mu.Unlock()

	if !stopped && e.onUnexpectedExit != nil {
		if failureErr != nil {
			e.onUnexpectedExit(failureErr)
		} else {
			e.onUnexpectedExit(err)
		}
	}
}

// fail makes a sing-box startup/runtime failure tear down the FreeTurn transport.
// This prevents the orchestrator from considering a transport-only session healthy.
func (e *FreeturnEngine) fail(err error) {
	e.mu.Lock()
	if e.userStopped || e.sessionClosing {
		e.mu.Unlock()
		return
	}
	if e.failureErr == nil {
		e.failureErr = err
	}
	e.sessionClosing = true
	cancel := e.cancel
	cmd := e.cmd
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (e *FreeturnEngine) Stop() {
	e.mu.Lock()
	e.userStopped = true
	e.sessionClosing = true
	cancel := e.cancel
	cmd := e.cmd
	exitChan := e.exitChan
	e.mu.Unlock()

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
	} else {
		e.mu.Lock()
		e.cleanupSingboxConfigLocked()
		e.mu.Unlock()
	}
}

func (e *FreeturnEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cmd != nil && !e.sessionClosing
}

func (e *FreeturnEngine) cleanupSingboxConfigLocked() {
	if e.sbCfgPath == "" {
		return
	}
	path := filepath.Clean(e.sbCfgPath)
	_ = os.Remove(path)
	e.sbCfgPath = ""
}
