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
	"time"
)

const singTunName = "fturn-tun"

// SingboxTun управляет процессом sing-box (TUN-интерфейс, маршруты, DNS).
type SingboxTun struct {
	appCtx   context.Context
	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	exitChan chan struct{}
	started  bool
	cfgPath  string
}

// Start проверяет конфиг и запускает sing-box run.
// Блокируется до появления "sing-box started" в логах или таймаута.
func (t *SingboxTun) Start(cfgPath string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cmd != nil {
		return fmt.Errorf("sing-box уже запущен")
	}

	sbPath := getSingboxPath()
	if sbPath == "" {
		return fmt.Errorf("ядро sing-box не найдено. Скачайте sing-box и поместите рядом с приложением")
	}

	// Проверка конфига
	if err := singboxCheck(sbPath, cfgPath); err != nil {
		return fmt.Errorf("невалидный конфиг sing-box: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.cfgPath = cfgPath
	t.exitChan = make(chan struct{})

	cmd := exec.CommandContext(ctx, sbPath, "run", "-c", cfgPath, "--disable-color")
	prepareProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	log.Printf("[SB] Запуск sing-box: %s run -c %s", sbPath, cfgPath)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("не удалось запустить sing-box: %w", err)
	}
	t.cmd = cmd

	// Привязка к Job Object (Windows: auto-kill при краше GUI)
	attachToJob(cmd)

	// Канал ожидания "sing-box started"
	readyCh := make(chan struct{}, 1)
	errCh := make(chan string, 1)

	// Парсинг логов (stdout + stderr мержим через MultiReader)
	go t.parseLogs(io.MultiReader(stdout, stderr), readyCh, errCh)

	// Ожидание готовности с таймаутом
	select {
	case <-readyCh:
		t.started = true
		log.Printf("[SB] sing-box запущен, TUN %s создан", singTunName)
	case errMsg := <-errCh:
		t.stopLocked()
		return fmt.Errorf("sing-box ошибка: %s", errMsg)
	case <-time.After(15 * time.Second):
		t.stopLocked()
		return fmt.Errorf("sing-box не запустился за 15 секунд")
	}

	// Мониторинг завершения процесса
	go func() {
		defer close(t.exitChan)
		_ = cmd.Wait()
		t.mu.Lock()
		t.cmd = nil
		t.started = false
		t.mu.Unlock()
		log.Printf("[SB] Процесс sing-box завершился")
	}()

	return nil
}

// Stop gracefully останавливает sing-box (CTRL_BREAK → 3s → Kill).
func (t *SingboxTun) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLocked()
}

func (t *SingboxTun) stopLocked() {
	if t.cmd == nil {
		return
	}

	log.Printf("[SB] Остановка sing-box...")

	// 1. Graceful: CTRL_BREAK (Windows) / SIGINT (Unix)
	if err := signalStop(t.cmd); err != nil {
		log.Printf("[SB] signalStop: %v, пробуем Kill", err)
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	} else {
		// Ждём 3 секунды для graceful shutdown
		done := make(chan struct{})
		go func() {
			if t.exitChan != nil {
				<-t.exitChan
			}
			close(done)
		}()

		select {
		case <-done:
			// Graceful shutdown прошёл
		case <-time.After(3 * time.Second):
			log.Printf("[SB] Graceful таймаут, Kill")
			if t.cmd.Process != nil {
				_ = t.cmd.Process.Kill()
			}
		}
	}

	// 2. Cancel context
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}

	t.cmd = nil
	t.started = false
}

// IsRunning возвращает true если sing-box запущен.
func (t *SingboxTun) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cmd != nil && t.started
}

// parseLogs читает stdout/stderr sing-box, детектирует ready и ошибки.
func (t *SingboxTun) parseLogs(r io.Reader, readyCh chan<- struct{}, errCh chan<- string) {
	scanner := bufio.NewScanner(r)
	readySent := false

	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[SB] %s", line)

		// Детекция успешного запуска
		if !readySent && strings.Contains(line, "sing-box started") {
			readySent = true
			select {
			case readyCh <- struct{}{}:
			default:
			}
		}

		// Детекция ошибок с подсказками
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") {
			hint := singboxHint(line)
			if hint != "" {
				log.Printf("[SB] 💡 %s", hint)
			}
			if !readySent {
				select {
				case errCh <- line:
				default:
				}
			}
		}
	}
}

// singboxHint возвращает русскоязычную подсказку по ошибке sing-box.
func singboxHint(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "reality") && strings.Contains(lower, "verify"):
		return "Reality: сервер отверг ClientHello — проверьте pbk/sid/sni"
	case strings.Contains(lower, "access is denied"):
		return "Нет прав на создание TUN — запустите от имени администратора"
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
