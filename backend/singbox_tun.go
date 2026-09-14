package backend

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const singTunName = "fturn-tun"

var (
	singboxPathFunc         = getSingboxPath
	singboxCheckFunc        = singboxCheck
	singboxVersionCheckFunc = validateSingboxVersion
	singboxCommandContext   = exec.CommandContext
	singboxSignalStopFunc   = signalStop
	singboxPrepareFunc      = prepareProcessGroup
	singboxAttachFunc       = attachToJob
	singboxReadyTimeout     = 15 * time.Second
	singboxStopTimeout      = 3 * time.Second
)

// SingboxTun manages the external sing-box process and its TUN lifecycle.
type SingboxTun struct {
	appCtx context.Context

	mu            sync.Mutex
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	done          chan struct{}
	started       bool
	starting      bool
	stopping      bool
	stopRequested bool
	cfgPath       string
	waitErr       error

	onUnexpectedExit func(error)
}

func (t *SingboxTun) Start(cfgPath string) error {
	parent := t.appCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	t.mu.Lock()
	if t.cmd != nil || t.starting {
		t.mu.Unlock()
		cancel()
		return fmt.Errorf("sing-box уже запущен или запускается")
	}
	t.starting = true
	t.stopping = false
	t.stopRequested = false
	t.cancel = cancel
	t.cfgPath = cfgPath
	t.waitErr = nil
	t.mu.Unlock()

	cleanupStartFailure := func() {
		t.mu.Lock()
		if t.cmd == nil {
			t.cancel = nil
		}
		t.starting = false
		t.started = false
		t.stopping = false
		t.mu.Unlock()
	}

	sbPath := singboxPathFunc()
	if sbPath == "" {
		cancel()
		cleanupStartFailure()
		return fmt.Errorf("ядро sing-box не найдено. Поместите sing-box рядом с приложением")
	}
	if err := singboxVersionCheckFunc(sbPath); err != nil {
		cancel()
		cleanupStartFailure()
		return fmt.Errorf("неподдерживаемый sing-box: %w", err)
	}

	select {
	case <-ctx.Done():
		cleanupStartFailure()
		return fmt.Errorf("запуск sing-box отменён: %w", ctx.Err())
	default:
	}
	if err := singboxCheckFunc(sbPath, cfgPath); err != nil {
		cancel()
		cleanupStartFailure()
		return fmt.Errorf("невалидный конфиг sing-box: %w", err)
	}
	// Stop/app cancellation may happen while sing-box check is running. Never
	// spawn the child after cancellation even if the checker ignored context.
	select {
	case <-ctx.Done():
		cleanupStartFailure()
		return fmt.Errorf("запуск sing-box отменён: %w", ctx.Err())
	default:
	}

	cmd := singboxCommandContext(ctx, sbPath, "run", "-c", cfgPath, "--disable-color")
	singboxPrepareFunc(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanupStartFailure()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		cleanupStartFailure()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	log.Printf("[SB] Запуск sing-box: %s run -c %s", sbPath, cfgPath)
	if err := cmd.Start(); err != nil {
		cancel()
		cleanupStartFailure()
		return fmt.Errorf("не удалось запустить sing-box: %w", err)
	}
	singboxAttachFunc(cmd)

	done := make(chan struct{})
	exitErrCh := make(chan error, 1)
	t.mu.Lock()
	t.cmd = cmd
	t.done = done
	wasStopRequested := t.stopRequested
	t.mu.Unlock()

	// Wait must begin immediately. Otherwise a process that exits before readiness
	// can leak a process handle/zombie and Stop waits on a channel nobody closes.
	go t.waitProcess(cmd, done, exitErrCh)

	var readySeen atomic.Bool
	readyCh := make(chan struct{})
	errCh := make(chan string, 2)
	var readyOnce sync.Once
	go t.parseLogs(stdout, &readySeen, &readyOnce, readyCh, errCh)
	go t.parseLogs(stderr, &readySeen, &readyOnce, readyCh, errCh)

	if wasStopRequested {
		t.stopProcess(cmd, cancel, done)
		cleanupStartFailure()
		return fmt.Errorf("запуск sing-box отменён")
	}

	timer := time.NewTimer(singboxReadyTimeout)
	defer timer.Stop()
	select {
	case <-readyCh:
		t.mu.Lock()
		if t.cmd != cmd || t.stopRequested || t.stopping {
			t.mu.Unlock()
			t.stopProcess(cmd, cancel, done)
			cleanupStartFailure()
			return fmt.Errorf("запуск sing-box отменён")
		}
		t.started = true
		t.starting = false
		t.mu.Unlock()
		log.Printf("[SB] sing-box запущен, TUN %s создан", singTunName)
		return nil
	case errMsg := <-errCh:
		t.stopProcess(cmd, cancel, done)
		cleanupStartFailure()
		return fmt.Errorf("sing-box ошибка: %s", errMsg)
	case err := <-exitErrCh:
		cleanupStartFailure()
		if err == nil {
			return fmt.Errorf("sing-box завершился до готовности")
		}
		return fmt.Errorf("sing-box завершился до готовности: %w", err)
	case <-ctx.Done():
		t.stopProcess(cmd, cancel, done)
		cleanupStartFailure()
		return fmt.Errorf("запуск sing-box отменён: %w", ctx.Err())
	case <-timer.C:
		t.stopProcess(cmd, cancel, done)
		cleanupStartFailure()
		return fmt.Errorf("sing-box не запустился за %s", singboxReadyTimeout)
	}
}

func (t *SingboxTun) waitProcess(cmd *exec.Cmd, done chan struct{}, exitErrCh chan<- error) {
	err := cmd.Wait()
	exitErrCh <- err
	close(done)

	t.mu.Lock()
	wasStarted := t.cmd == cmd && t.started
	parentCancelled := t.appCtx != nil && t.appCtx.Err() != nil
	unexpected := t.cmd == cmd && wasStarted && !t.stopping && !t.stopRequested && !parentCancelled
	if t.cmd == cmd {
		t.waitErr = err
		t.cmd = nil
		t.cancel = nil
		t.done = nil
		t.started = false
		t.starting = false
	}
	callback := t.onUnexpectedExit
	t.mu.Unlock()

	log.Printf("[SB] Процесс sing-box завершился (err: %v)", err)
	if unexpected && callback != nil {
		if err == nil {
			callback(fmt.Errorf("sing-box exited unexpectedly"))
		} else {
			callback(err)
		}
	}
}

// Stop gracefully stops sing-box without holding the state mutex while waiting.
func (t *SingboxTun) Stop() {
	t.mu.Lock()
	t.stopRequested = true
	t.stopping = true
	cmd := t.cmd
	cancel := t.cancel
	done := t.done
	starting := t.starting
	t.mu.Unlock()

	if cmd == nil {
		if starting && cancel != nil {
			cancel()
			// The Start goroutine owns clearing starting/stopping after it observes
			// cancellation. Keeping starting=true here prevents a second Start from
			// racing the still-unwinding first one.
			return
		}
		t.mu.Lock()
		t.started = false
		t.stopping = false
		t.mu.Unlock()
		return
	}

	t.stopProcess(cmd, cancel, done)
	t.mu.Lock()
	if t.cmd == cmd {
		t.cmd = nil
		t.cancel = nil
		t.done = nil
	}
	t.started = false
	t.starting = false
	t.stopping = false
	t.mu.Unlock()
}

func (t *SingboxTun) stopProcess(cmd *exec.Cmd, cancel context.CancelFunc, done <-chan struct{}) {
	if cmd == nil {
		if cancel != nil {
			cancel()
		}
		return
	}
	log.Printf("[SB] Остановка sing-box...")

	if err := singboxSignalStopFunc(cmd); err != nil {
		log.Printf("[SB] signalStop: %v, завершаем процесс", err)
		if cancel != nil {
			cancel()
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	} else if done != nil {
		timer := time.NewTimer(singboxStopTimeout)
		select {
		case <-done:
			timer.Stop()
			return
		case <-timer.C:
			log.Printf("[SB] Graceful таймаут, Kill")
			if cancel != nil {
				cancel()
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	}

	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			log.Printf("[SB] процесс не подтвердил завершение после Kill")
		}
	}
}

func (t *SingboxTun) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cmd != nil && t.started
}

func (t *SingboxTun) parseLogs(r io.Reader, readySeen *atomic.Bool, readyOnce *sync.Once, readyCh chan<- struct{}, errCh chan<- string) {
	scanner := bufio.NewScanner(r)
	// sing-box can print long JSON/error lines; the Scanner default is only 64 KiB.
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[SB] %s", boundedLogLine(line, 4096))
		lower := strings.ToLower(line)
		if strings.Contains(lower, "sing-box started") {
			readySeen.Store(true)
			readyOnce.Do(func() { close(readyCh) })
			continue
		}
		if (strings.Contains(lower, "error") || strings.Contains(lower, "fatal")) && !readySeen.Load() {
			if hint := singboxHint(line); hint != "" {
				log.Printf("[SB] Подсказка: %s", hint)
			}
			select {
			case errCh <- line:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil && !readySeen.Load() {
		select {
		case errCh <- "ошибка чтения логов sing-box: " + err.Error():
		default:
		}
	}
}

func boundedLogLine(line string, maxBytes int) string {
	if maxBytes <= 0 || len(line) <= maxBytes {
		return line
	}
	return line[:maxBytes] + fmt.Sprintf("... [truncated %d bytes]", len(line)-maxBytes)
}

func singboxHint(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "reality") && strings.Contains(lower, "verify"):
		return "Reality: сервер отверг ClientHello — проверьте pbk/sid/sni"
	case strings.Contains(lower, "access is denied") || strings.Contains(lower, "permission denied"):
		return "Нет прав на создание TUN — нужны Administrator/root/CAP_NET_ADMIN"
	case strings.Contains(lower, "configure tun interface"):
		return "Конфликт с другим VPN или занято имя интерфейса"
	case strings.Contains(lower, "address already in use"):
		return "Порт уже занят другим процессом"
	case strings.Contains(lower, "tls handshake"):
		return "Ошибка TLS — проверьте SNI и сертификат сервера"
	default:
		return ""
	}
}
