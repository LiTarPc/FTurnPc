package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

const minSingboxMajor = 1
const minSingboxMinor = 14

var singboxVersionRE = regexp.MustCompile(`(?m)\b(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?\b`)

// getSingboxPath locates the external sing-box executable on all supported OSes.
func getSingboxPath() string {
	binaryName := "sing-box"
	if goruntime.GOOS == "windows" {
		binaryName = "sing-box.exe"
	}

	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, binaryName),
		filepath.Join(exeDir, "assets", "singbox", binaryName),
		filepath.Join(configDir(), "core", binaryName),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p
	}
	return ""
}

func singboxVersion() string {
	path := getSingboxPath()
	if path == "" {
		return "Не установлен"
	}
	version, err := singboxVersionAt(path)
	if err != nil {
		return "Не установлен"
	}
	return version
}

func singboxVersionAt(exePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exePath, "version")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("sing-box version timeout: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("sing-box version: %w: %s", err, strings.TrimSpace(string(out)))
	}
	match := singboxVersionRE.FindStringSubmatch(string(out))
	if len(match) != 4 {
		return "", fmt.Errorf("не удалось определить версию sing-box: %q", strings.TrimSpace(string(out)))
	}
	return match[0], nil
}

func validateSingboxVersion(exePath string) error {
	version, err := singboxVersionAt(exePath)
	if err != nil {
		return err
	}
	match := singboxVersionRE.FindStringSubmatch(version)
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < minSingboxMajor || (major == minSingboxMajor && minor < minSingboxMinor) {
		return fmt.Errorf("sing-box %s слишком старый: требуется >= %d.%d.0", version, minSingboxMajor, minSingboxMinor)
	}
	return nil
}

// singboxCheck validates config with a bounded timeout so startup cannot hang forever.
func singboxCheck(exePath, cfgPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return singboxCheckContext(ctx, exePath, cfgPath)
}

func singboxCheckContext(ctx context.Context, exePath, cfgPath string) error {
	cmd := exec.CommandContext(ctx, exePath, "check", "-c", cfgPath)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("sing-box check timeout/cancelled: %w", ctx.Err())
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("sing-box check: %s", msg)
	}
	return nil
}
