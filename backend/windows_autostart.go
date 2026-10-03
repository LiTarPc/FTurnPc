//go:build windows

package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey        = `Software\Microsoft\Windows\CurrentVersion\Run`
	legacyRunName = "PWDTT"
	autoStartTask = "FTurnPc-singbox-AutoStart"
)

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func runTaskPowerShell(script string) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(utf16LE(script))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Task Scheduler: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func utf16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	result := make([]byte, len(units)*2)
	for i, unit := range units {
		result[2*i] = byte(unit)
		result[2*i+1] = byte(unit >> 8)
	}
	return result
}

func scheduledAutoStartEnabled(exe string) bool {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$task = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue
if ($null -ne $task -and $task.State -ne 'Disabled' -and @($task.Actions | Where-Object { $_.Execute -ieq %s }).Count -gt 0) { 'true' } else { 'false' }`, psQuote(autoStartTask), psQuote(exe))
	out, err := runTaskPowerShell(script)
	return err == nil && out == "true"
}

func legacyAutoStartFor(exe string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	value, _, err := k.GetStringValue(legacyRunName)
	return err == nil && strings.EqualFold(strings.Trim(value, `"`), exe)
}

func removeLegacyAutoStart(exe string) error {
	if !legacyAutoStartFor(exe) {
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(legacyRunName)
}

func (a *App) SetAutoStart(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if enabled {
		// The GUI needs an interactive desktop and an elevated token for TUN.
		// Registering once while elevated avoids a UAC prompt at every logon.
		script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$action = New-ScheduledTaskAction -Execute %s -WorkingDirectory %s
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $sid
$principal = New-ScheduledTaskPrincipal -UserId $sid -LogonType Interactive -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName %s -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null`, psQuote(exe), psQuote(filepath.Dir(exe)), psQuote(autoStartTask))
		if _, err := runTaskPowerShell(script); err != nil {
			return err
		}
		return removeLegacyAutoStart(exe)
	}

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$task = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue
if ($null -ne $task) { Unregister-ScheduledTask -TaskName %s -Confirm:$false -ErrorAction Stop }`, psQuote(autoStartTask), psQuote(autoStartTask))
	if _, err := runTaskPowerShell(script); err != nil {
		return err
	}
	return removeLegacyAutoStart(exe)
}

func (a *App) GetAutoStart() bool {
	exe, err := os.Executable()
	return err == nil && scheduledAutoStartEnabled(exe)
}

// MigrateWindowsAutoStart replaces the old HKCU Run entry when it belongs to
// this executable. The old entry started an admin-manifest GUI with UAC every
// time the user logged on.
func MigrateWindowsAutoStart() error {
	exe, err := os.Executable()
	if err != nil || !legacyAutoStartFor(exe) {
		return err
	}
	return (&App{}).SetAutoStart(true)
}
