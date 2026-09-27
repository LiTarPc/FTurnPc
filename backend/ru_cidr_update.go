package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ruCIDRURL          = "https://www.ipdeny.com/ipblocks/data/aggregated/ru-aggregated.zone"
	maxRuCIDRBytes     = 2 * 1024 * 1024
	minRuCIDRCount     = 1000
	maxRuCIDRCount     = 20000
)

var ruCIDRUpdateMu sync.Mutex

type RuCIDRStatus struct {
	Count     int    `json:"count"`
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func parseRuCIDRs(data []byte) ([]string, error) {
	if len(data) == 0 || len(data) > maxRuCIDRBytes {
		return nil, fmt.Errorf("RU CIDR: недопустимый размер файла")
	}
	lines := strings.Split(string(data), "\n")
	result := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil || !prefix.Addr().Is4() || prefix.Masked().String() != line {
			return nil, fmt.Errorf("RU CIDR: некорректная IPv4-сеть в строке %d", i+1)
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		result = append(result, line)
		if len(result) > maxRuCIDRCount {
			return nil, fmt.Errorf("RU CIDR: слишком много сетей")
		}
	}
	return result, nil
}

func ruCIDRPaths() (txt, srs string) {
	return filepath.Join(configDir(), "geoip-ru.txt"), filepath.Join(configDir(), "geoip-ru.srs")
}

func (a *App) GetRuCIDRStatus() RuCIDRStatus {
	txt, srs := ruCIDRPaths()
	if st, err := os.Stat(srs); err == nil && st.Size() > 0 {
		if data, err := os.ReadFile(txt); err == nil {
			if cidrs, err := parseRuCIDRs(data); err == nil && len(cidrs) >= minRuCIDRCount {
				if txtInfo, err := os.Stat(txt); err == nil {
					return RuCIDRStatus{Count: len(cidrs), Source: "IPdeny", UpdatedAt: txtInfo.ModTime().UTC().Format(time.RFC3339)}
				}
			}
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return RuCIDRStatus{Source: "Встроенный"}
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "geoip-ru.txt"))
	if err != nil {
		return RuCIDRStatus{Source: "Встроенный"}
	}
	cidrs, _ := parseRuCIDRs(data)
	return RuCIDRStatus{Count: len(cidrs), Source: "Встроенный"}
}

func (a *App) UpdateRuCIDR() (RuCIDRStatus, error) {
	ruCIDRUpdateMu.Lock()
	defer ruCIDRUpdateMu.Unlock()
	if a.orch != nil && a.orch.sessionActive() {
		return RuCIDRStatus{}, fmt.Errorf("отключите туннель перед обновлением RU CIDR")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" {
			return fmt.Errorf("RU CIDR: небезопасное перенаправление")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ruCIDRURL, nil)
	if err != nil {
		return RuCIDRStatus{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return RuCIDRStatus{}, fmt.Errorf("загрузка RU CIDR: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RuCIDRStatus{}, fmt.Errorf("загрузка RU CIDR: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRuCIDRBytes+1))
	if err != nil {
		return RuCIDRStatus{}, fmt.Errorf("чтение RU CIDR: %w", err)
	}
	cidrs, err := parseRuCIDRs(data)
	if err != nil {
		return RuCIDRStatus{}, err
	}
	if len(cidrs) < minRuCIDRCount {
		return RuCIDRStatus{}, fmt.Errorf("RU CIDR: получено только %d сетей; старый список сохранён", len(cidrs))
	}
	if a.orch != nil {
		a.orch.transitionMu.Lock()
		defer a.orch.transitionMu.Unlock()
		if a.orch.sessionActive() {
			return RuCIDRStatus{}, fmt.Errorf("подключение запущено во время обновления RU CIDR; попробуйте после отключения")
		}
	}
	if err := installRuCIDRs(ctx, cidrs); err != nil {
		return RuCIDRStatus{}, err
	}
	return a.GetRuCIDRStatus(), nil
}

func installRuCIDRs(ctx context.Context, cidrs []string) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stageDir, err := os.MkdirTemp(dir, "ru-cidr-*")
	if err != nil {
		return err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stageDir)
		}
	}()

	txtStage := filepath.Join(stageDir, "geoip-ru.txt")
	srsStage := filepath.Join(stageDir, "geoip-ru.srs")
	jsonStage := filepath.Join(stageDir, "geoip-ru.json")
	text := []byte(strings.Join(cidrs, "\n") + "\n")
	if err := os.WriteFile(txtStage, text, 0o600); err != nil {
		return err
	}
	source, err := json.Marshal(map[string]interface{}{
		"version": 5,
		"rules": []interface{}{map[string]interface{}{"ip_cidr": cidrs}},
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonStage, source, 0o600); err != nil {
		return err
	}
	sbPath := getSingboxPath()
	if sbPath == "" {
		return fmt.Errorf("sing-box не найден; RU CIDR не обновлены")
	}
	cmd := exec.CommandContext(ctx, sbPath, "rule-set", "compile", jsonStage, "--output", srsStage)
	hideWindow(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("компиляция RU CIDR: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if st, err := os.Stat(srsStage); err != nil || st.Size() == 0 {
		return fmt.Errorf("sing-box не создал RU rule-set")
	}
	if err := os.Chmod(srsStage, 0o600); err != nil {
		return err
	}
	txtTarget, srsTarget := ruCIDRPaths()
	if err := replaceRuRuleFiles(stageDir, [][2]string{{srsStage, srsTarget}, {txtStage, txtTarget}}); err != nil {
		keepStage = true // Keep backups available if Windows could not restore them.
		return fmt.Errorf("установка RU CIDR: %w (резервные файлы: %s)", err, stageDir)
	}
	return nil
}

// The text file is the marker that a downloaded rule-set is installed. Move
// the SRS first; on any ordinary error restore both previous files.
func replaceRuRuleFiles(stageDir string, files [][2]string) error {
	type changedFile struct {
		target, backup string
		hadOld bool
	}
	var changed []changedFile
	rollback := func() {
		for i := len(changed) - 1; i >= 0; i-- {
			item := changed[i]
			_ = os.Remove(item.target)
			if item.hadOld {
				_ = os.Rename(item.backup, item.target)
			}
		}
	}
	for i, pair := range files {
		item := changedFile{target: pair[1], backup: filepath.Join(stageDir, fmt.Sprintf("backup-%d", i))}
		if _, err := os.Stat(item.target); err == nil {
			if err := os.Rename(item.target, item.backup); err != nil {
				rollback()
				return err
			}
			item.hadOld = true
		} else if !os.IsNotExist(err) {
			rollback()
			return err
		}
		changed = append(changed, item)
		if err := os.Rename(pair[0], item.target); err != nil {
			rollback()
			return err
		}
	}
	return nil
}
