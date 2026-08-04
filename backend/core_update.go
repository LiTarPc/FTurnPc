package backend

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CoreRepo указывают GitHub репозиторий для автоматического подтягивания релизов ядра
const CoreRepo = "samosvalishe/free-turn-proxy"

type CoreUpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	DownloadURL    string `json:"downloadUrl"`
	ReleaseNotes   string `json:"releaseNotes"`
	PublishedAt    string `json:"publishedAt"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubReleaseResponse struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	PublishedAt string               `json:"published_at"`
	Body        string               `json:"body"`
	Assets      []githubReleaseAsset `json:"assets"`
}

// GetCoreVersion пытается получить текущую версию freeturnclient
func GetCoreVersion() string {
	exePath := getFreeturnPath()
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return "Не установлен"
	}

	// 1. Попробуем выполнить freeturnclient -version или -h
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, exePath, "-version")
	out, err := cmd.CombinedOutput()
	if err == nil && len(out) > 0 {
		ver := strings.TrimSpace(string(out))
		if ver != "" {
			return ver
		}
	}

	// 2. Если аргумент -version не вернул данных, покажем дату изменения файла
	fi, err := os.Stat(exePath)
	if err == nil {
		return fmt.Sprintf("Бинарный файл от %s", fi.ModTime().Format("2006-01-02"))
	}

	return "Установлен"
}

// CheckCoreUpdate проверяет наличие обновлений ядра на GitHub репозитории CoreRepo
func CheckCoreUpdate() (CoreUpdateInfo, error) {
	currentVer := GetCoreVersion()
	info := CoreUpdateInfo{
		CurrentVersion: currentVer,
	}

	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", CoreRepo)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("User-Agent", "FTurnPc-App")

	resp, err := client.Do(req)
	if err != nil {
		return info, fmt.Errorf("ошибка запроса к GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("GitHub API вернул статус %d", resp.StatusCode)
	}

	var rel githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info, fmt.Errorf("ошибка декодирования ответа GitHub: %w", err)
	}

	info.LatestVersion = rel.TagName
	info.ReleaseNotes = rel.Body
	info.PublishedAt = rel.PublishedAt

	// Ищем наиболее подходящий ассет для текущей ОС и архитектуры
	downloadURL := ""
	goos := goruntime.GOOS
	goarch := goruntime.GOARCH

	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)

		// Сопоставление с ОС и архитектурой (учитывая как freeturnclient, так и client-windows-amd64)
		matchOS := strings.Contains(name, goos) ||
			(goos == "windows" && (strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".zip") || strings.Contains(name, "win")))
		matchArch := strings.Contains(name, goarch) ||
			(goarch == "amd64" && (strings.Contains(name, "amd64") || strings.Contains(name, "x64") || strings.Contains(name, "64")))

		if matchOS && (matchArch || len(rel.Assets) == 1) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	// Если специфичный ассет не найден, берем первый попавшийся подходящий по расширению
	if downloadURL == "" {
		for _, asset := range rel.Assets {
			name := strings.ToLower(asset.Name)
			if goos == "windows" && (strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".zip")) {
				downloadURL = asset.BrowserDownloadURL
				break
			} else if goos != "windows" && !strings.HasSuffix(name, ".exe") && !strings.HasSuffix(name, ".zip") {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}
	}

	// Если у нас еще вообще нет версии или версия отличается
	info.DownloadURL = downloadURL
	if downloadURL != "" && (currentVer == "Не установлен" || !strings.Contains(currentVer, rel.TagName)) {
		info.HasUpdate = true
	}

	return info, nil
}

// UpdateCore скачивает и обновляет freeturnclient
func UpdateCore(ctx context.Context, downloadURL string) error {
	if downloadURL == "" {
		info, err := CheckCoreUpdate()
		if err != nil {
			return err
		}
		if info.DownloadURL == "" {
			return fmt.Errorf("не удалось найти подходящий дистрибутив для скачивания")
		}
		downloadURL = info.DownloadURL
	}

	log.Printf("[CoreUpdate] Загрузка обновления ядра с %s...", downloadURL)
	runtime.EventsEmit(ctx, "core_update_progress", 5)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "FTurnPc-App")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось скачать обновление: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ошибка загрузки (HTTP %d)", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	var downloaded int64

	buf := make([]byte, 32*1024)
	var bodyBytes bytes.Buffer

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			bodyBytes.Write(buf[:n])
			downloaded += int64(n)
			if totalSize > 0 {
				percent := int(float64(downloaded) / float64(totalSize) * 80)
				runtime.EventsEmit(ctx, "core_update_progress", 10+percent)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ошибка при чтении потока: %w", err)
		}
	}

	runtime.EventsEmit(ctx, "core_update_progress", 90)

	// Определение формата скачанных данных (ZIP или Raw Executable)
	var exeBytes []byte
	isZip := strings.HasSuffix(strings.ToLower(downloadURL), ".zip") || bytes.HasPrefix(bodyBytes.Bytes(), []byte("PK\x03\x04"))

	if isZip {
		zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes.Bytes()), int64(bodyBytes.Len()))
		if err != nil {
			return fmt.Errorf("ошибка чтения zip-архива: %w", err)
		}

		// Ищем любой подходящий исполняемый файл внутри ZIP (client-windows-amd64.exe, freeturnclient.exe, client.exe и т.д.)
		var candidate *zip.File
		for _, f := range zipReader.File {
			if f.FileInfo().IsDir() {
				continue
			}
			fName := strings.ToLower(filepath.Base(f.Name))

			// Если Windows — отдаем приоритет .exe файлам или файлам с client
			if goruntime.GOOS == "windows" {
				if strings.HasSuffix(fName, ".exe") || strings.Contains(fName, "client") {
					candidate = f
					break
				}
			} else {
				if strings.Contains(fName, "client") || !strings.Contains(fName, ".") {
					candidate = f
					break
				}
			}
		}

		// Если явно с "client" не нашли, берем первый попавшийся не папочный файл
		if candidate == nil {
			for _, f := range zipReader.File {
				if !f.FileInfo().IsDir() {
					candidate = f
					break
				}
			}
		}

		if candidate == nil {
			return fmt.Errorf("в zip-архиве не найдено подходящих файлов для установки")
		}

		rc, err := candidate.Open()
		if err != nil {
			return fmt.Errorf("ошибка открытия файла %s в zip: %w", candidate.Name, err)
		}
		extracted, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("ошибка распаковки %s: %w", candidate.Name, err)
		}
		exeBytes = extracted
	} else {
		exeBytes = bodyBytes.Bytes()
	}

	// Определение целевого пути для установки
	targetPath := getFreeturnPath()
	if targetPath == "" || strings.Contains(targetPath, "LookPath") {
		exe, _ := os.Executable()
		exeName := "freeturnclient"
		if goruntime.GOOS == "windows" {
			exeName = "freeturnclient.exe"
		}
		targetPath = filepath.Join(filepath.Dir(exe), exeName)
	}

	log.Printf("[CoreUpdate] Запись нового исполняемого файла в %s (%d байт)...", targetPath, len(exeBytes))

	// Замена файла (с бэкапом старого для Windows)
	oldBackupPath := targetPath + ".old"
	_ = os.Remove(oldBackupPath)

	if _, err := os.Stat(targetPath); err == nil {
		_ = os.Rename(targetPath, oldBackupPath)
	}

	if err := os.WriteFile(targetPath, exeBytes, 0755); err != nil {
		// Откат если не удалось записать новый файл
		if _, statErr := os.Stat(oldBackupPath); statErr == nil {
			_ = os.Rename(oldBackupPath, targetPath)
		}
		return fmt.Errorf("не удалось записать файл ядра: %w", err)
	}

	_ = os.Remove(oldBackupPath)

	runtime.EventsEmit(ctx, "core_update_progress", 100)
	runtime.EventsEmit(ctx, "core_update_done", "success")
	log.Printf("[CoreUpdate] Ядро FreeTurn успешно обновлено!")
	return nil
}
