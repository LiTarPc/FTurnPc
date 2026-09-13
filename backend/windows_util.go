//go:build windows

package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runWithTimeout(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runWithOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// getSingboxPath ищет бинарь sing-box в стандартных расположениях.
func getSingboxPath() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)

	candidates := []string{
		filepath.Join(exeDir, "sing-box.exe"),
		filepath.Join(exeDir, "assets", "singbox", "sing-box.exe"),
		filepath.Join(configDir(), "core", "sing-box.exe"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Поиск в PATH
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p
	}
	return ""
}

// singboxVersion возвращает версию sing-box или "Не установлен".
func singboxVersion() string {
	sbPath := getSingboxPath()
	if sbPath == "" {
		return "Не установлен"
	}
	out, err := runWithOutput(sbPath, "version")
	if err != nil {
		return "Не установлен"
	}
	// Первая строка обычно: "sing-box version 1.14.0"
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		if idx := strings.LastIndex(line, " "); idx != -1 {
			return strings.TrimSpace(line[idx+1:])
		}
		return line
	}
	return "Не установлен"
}

// singboxCheck проверяет конфиг sing-box на валидность.
func singboxCheck(exePath, cfgPath string) error {
	out, err := runWithOutput(exePath, "check", "-c", cfgPath)
	if err != nil {
		return fmt.Errorf("sing-box check: %s", strings.TrimSpace(out))
	}
	return nil
}

// initJob — заглушка. Реальный Job Object создаётся в proc_windows.go.
var jobOnce sync.Once
