package backend

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const CoreRepo = "samosvalishe/free-turn-proxy"

const (
	maxCoreDownloadBytes   int64 = 64 * 1024 * 1024
	maxCoreExecutableBytes int64 = 50 * 1024 * 1024
	maxGitHubJSONBytes     int64 = 2 * 1024 * 1024
)

type CoreUpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	DownloadURL    string `json:"downloadUrl"`
	SHA256         string `json:"sha256,omitempty"`
	ReleaseNotes   string `json:"releaseNotes"`
	PublishedAt    string `json:"publishedAt"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type githubReleaseResponse struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	PublishedAt string               `json:"published_at"`
	Body        string               `json:"body"`
	Assets      []githubReleaseAsset `json:"assets"`
}

func GetCoreVersion() string {
	exePath := getFreeturnPath()
	fi, err := os.Stat(exePath)
	if err != nil || fi.IsDir() {
		return "Не установлен"
	}

	verFile := filepath.Join(filepath.Dir(exePath), "core_version.txt")
	if vfi, err := os.Stat(verFile); err == nil && !vfi.ModTime().Before(fi.ModTime()) {
		if data, err := os.ReadFile(verFile); err == nil {
			if ver := strings.TrimSpace(string(data)); ver != "" {
				return ver
			}
		}
	}

	cacheVersion := func(ver string) string {
		ver = strings.TrimSpace(ver)
		if ver != "" {
			_ = os.WriteFile(verFile, []byte(ver), 0o644)
		}
		return ver
	}

	if bi, err := buildinfo.ReadFile(exePath); err == nil {
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return cacheVersion(bi.Main.Version)
		}
		for _, s := range bi.Settings {
			if s.Key != "vcs.revision" {
				continue
			}
			switch {
			case strings.HasPrefix(s.Value, "fa9549e6"):
				return cacheVersion("v3.2.0")
			case strings.HasPrefix(s.Value, "aed2839c"):
				return cacheVersion("v3.1.1")
			}
			break
		}
	}

	ctxHelp, cancelHelp := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelHelp()
	cmdHelp := exec.CommandContext(ctxHelp, exePath, "-help")
	hideWindow(cmdHelp)
	outHelp, _ := cmdHelp.CombinedOutput()
	outStr := string(outHelp)
	switch {
	case strings.Contains(outStr, "-platform") || strings.Contains(outStr, "-routes"):
		return cacheVersion("v3.2.0")
	case strings.Contains(outStr, "-dns-mode") || strings.Contains(outStr, "-manual-captcha"):
		return "v3.1.x"
	case strings.Contains(outStr, "-mode"):
		return "v2.x.x"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exePath, "-gen-obf-key")
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.Index(line, "version=")
		if idx < 0 {
			continue
		}
		ver := strings.TrimSpace(line[idx+len("version="):])
		if ver == "" {
			continue
		}
		if !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		return cacheVersion(ver)
	}

	return fmt.Sprintf("Бинарный файл от %s", fi.ModTime().Format("2006-01-02"))
}

func CheckCoreUpdate() (CoreUpdateInfo, error) {
	current := GetCoreVersion()
	info := CoreUpdateInfo{CurrentVersion: current}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	rel, err := fetchGitHubRelease(ctx, "latest")
	if err != nil {
		return info, err
	}
	info.LatestVersion = rel.TagName
	info.ReleaseNotes = rel.Body
	info.PublishedAt = rel.PublishedAt

	asset := selectCoreAsset(rel.Assets, goruntime.GOOS, goruntime.GOARCH)
	if asset == nil {
		return info, nil
	}
	if err := validateCoreDownloadURL(asset.BrowserDownloadURL); err != nil {
		return info, fmt.Errorf("release asset URL: %w", err)
	}
	sha, err := parseSHA256Digest(asset.Digest)
	if err != nil {
		return info, fmt.Errorf("release asset %s: %w", asset.Name, err)
	}
	info.DownloadURL = asset.BrowserDownloadURL
	info.SHA256 = sha
	info.HasUpdate = coreHasUpdate(current, rel.TagName)
	return info, nil
}

func coreHasUpdate(current, latest string) bool {
	if strings.TrimSpace(latest) == "" {
		return false
	}
	if current == "Не установлен" || current == "Установлен" || strings.HasPrefix(current, "Бинарный файл") {
		return true
	}
	norm := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
	return norm(current) != norm(latest)
}

func fetchGitHubRelease(ctx context.Context, tag string) (githubReleaseResponse, error) {
	var endpoint string
	if tag == "latest" {
		endpoint = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", CoreRepo)
	} else {
		endpoint = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", CoreRepo, url.PathEscape(tag))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubReleaseResponse{}, err
	}
	req.Header.Set("User-Agent", "FTurnPc-App")
	resp, err := coreHTTPClient(12 * time.Second).Do(req)
	if err != nil {
		return githubReleaseResponse{}, fmt.Errorf("ошибка запроса к GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubReleaseResponse{}, fmt.Errorf("GitHub API вернул статус %d", resp.StatusCode)
	}
	data, err := readLimited(resp.Body, maxGitHubJSONBytes)
	if err != nil {
		return githubReleaseResponse{}, fmt.Errorf("GitHub release response: %w", err)
	}
	var rel githubReleaseResponse
	if err := json.Unmarshal(data, &rel); err != nil {
		return githubReleaseResponse{}, fmt.Errorf("ошибка декодирования ответа GitHub: %w", err)
	}
	return rel, nil
}

func coreHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("слишком много HTTP redirect")
			}
			if req.URL.Scheme != "https" || !allowedCoreRedirectHost(req.URL.Hostname()) {
				return fmt.Errorf("неразрешённый redirect host: %s", req.URL.String())
			}
			return nil
		},
	}
}

func allowedCoreRedirectHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "api.github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	default:
		return false
	}
}

func selectCoreAsset(assets []githubReleaseAsset, goos, goarch string) *githubReleaseAsset {
	wanted := expectedCoreAssetNames(goos, goarch)
	for _, want := range wanted {
		for i := range assets {
			if strings.EqualFold(filepath.Base(assets[i].Name), want) {
				return &assets[i]
			}
		}
	}
	return nil
}

func expectedCoreAssetNames(goos, goarch string) []string {
	arch := goarch
	switch goarch {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	case "386":
		arch = "386"
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return []string{
		"client-" + goos + "-" + arch + ext,
		"freeturnclient-" + goos + "-" + arch + ext,
	}
}

func validateCoreDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("невалидный URL: %w", err)
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") {
		return fmt.Errorf("разрешён только HTTPS github.com")
	}
	prefix := "/" + CoreRepo + "/releases/download/"
	if !strings.HasPrefix(u.EscapedPath(), prefix) && !strings.HasPrefix(u.Path, prefix) {
		return fmt.Errorf("URL не относится к releases/download репозитория %s", CoreRepo)
	}
	return nil
}

func releaseAssetForURL(ctx context.Context, raw string) (githubReleaseAsset, string, error) {
	if err := validateCoreDownloadURL(raw); err != nil {
		return githubReleaseAsset{}, "", err
	}
	u, _ := url.Parse(raw)
	prefix := "/" + CoreRepo + "/releases/download/"
	rest := strings.TrimPrefix(u.Path, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return githubReleaseAsset{}, "", fmt.Errorf("не удалось извлечь tag/asset из URL")
	}
	tag, assetName := parts[0], filepath.Base(parts[1])
	rel, err := fetchGitHubRelease(ctx, tag)
	if err != nil {
		return githubReleaseAsset{}, "", err
	}
	for _, asset := range rel.Assets {
		if asset.Name == assetName && asset.BrowserDownloadURL == raw {
			if _, err := parseSHA256Digest(asset.Digest); err != nil {
				return githubReleaseAsset{}, "", fmt.Errorf("asset digest: %w", err)
			}
			return asset, rel.TagName, nil
		}
	}
	return githubReleaseAsset{}, "", fmt.Errorf("asset %q не найден в release %q", assetName, tag)
}

func parseSHA256Digest(digest string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(digest), ":", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "sha256") {
		return "", fmt.Errorf("ожидается sha256 digest, получено %q", digest)
	}
	h := strings.ToLower(parts[1])
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("невалидный SHA-256 digest")
	}
	return h, nil
}

func UpdateCore(ctx context.Context, downloadURL string, beforeReplaceFn func()) error {
	var asset githubReleaseAsset
	var targetVer string
	var expectedSHA string
	var err error

	if strings.TrimSpace(downloadURL) == "" {
		info, checkErr := CheckCoreUpdate()
		if checkErr != nil {
			return checkErr
		}
		if info.DownloadURL == "" {
			return fmt.Errorf("не удалось найти подходящий дистрибутив для %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
		downloadURL = info.DownloadURL
		targetVer = info.LatestVersion
		expectedSHA = info.SHA256
		asset = githubReleaseAsset{Name: filepath.Base(downloadURL), BrowserDownloadURL: downloadURL}
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		asset, targetVer, err = releaseAssetForURL(lookupCtx, downloadURL)
		if err != nil {
			return fmt.Errorf("проверка release asset: %w", err)
		}
		expectedSHA, _ = parseSHA256Digest(asset.Digest)
	}

	if selected := selectCoreAsset([]githubReleaseAsset{asset}, goruntime.GOOS, goruntime.GOARCH); selected == nil {
		return fmt.Errorf("asset %q не соответствует платформе %s/%s", asset.Name, goruntime.GOOS, goruntime.GOARCH)
	}

	log.Printf("[CoreUpdate] Загрузка %s...", downloadURL)
	runtime.EventsEmit(ctx, "core_update_progress", 5, "Подключение к GitHub...")
	body, err := downloadCoreAsset(ctx, downloadURL)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(body)
	actualSHA := hex.EncodeToString(actual[:])
	if expectedSHA == "" || !strings.EqualFold(actualSHA, expectedSHA) {
		return fmt.Errorf("SHA-256 не совпадает: expected=%s actual=%s", expectedSHA, actualSHA)
	}

	runtime.EventsEmit(ctx, "core_update_progress", 92, "Проверка пакета...")
	exeBytes, err := extractCoreExecutable(body, downloadURL, goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		return err
	}
	if err := validateExecutableBytes(exeBytes, goruntime.GOOS); err != nil {
		return err
	}

	if beforeReplaceFn != nil {
		runtime.EventsEmit(ctx, "core_update_progress", 95, "Остановка сессии перед заменой...")
		beforeReplaceFn()
	}

	targetPath := coreInstallPath()
	runtime.EventsEmit(ctx, "core_update_progress", 97, "Установка файла ядра...")
	if err := replaceExecutableAtomically(targetPath, exeBytes); err != nil {
		return err
	}

	if targetVer == "" {
		targetVer = GetCoreVersion()
	}
	if targetVer != "" {
		_ = os.WriteFile(filepath.Join(filepath.Dir(targetPath), "core_version.txt"), []byte(targetVer), 0o644)
	}
	runtime.EventsEmit(ctx, "core_update_progress", 100, "Ядро успешно обновлено!")
	runtime.EventsEmit(ctx, "core_update_done", targetVer)
	log.Printf("[CoreUpdate] Ядро FreeTurn обновлено до %s", targetVer)
	return nil
}

func downloadCoreAsset(ctx context.Context, rawURL string) ([]byte, error) {
	if err := validateCoreDownloadURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FTurnPc-App")
	resp, err := coreHTTPClient(5 * time.Minute).Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось скачать обновление: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ошибка загрузки (HTTP %d)", resp.StatusCode)
	}
	if resp.ContentLength > maxCoreDownloadBytes {
		return nil, fmt.Errorf("release asset слишком большой: %d bytes", resp.ContentLength)
	}

	var out bytes.Buffer
	buf := make([]byte, 64*1024)
	var downloaded int64
	lastEmit := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxCoreDownloadBytes {
				return nil, fmt.Errorf("release asset превышает лимит %d bytes", maxCoreDownloadBytes)
			}
			_, _ = out.Write(buf[:n])
			if resp.ContentLength > 0 && time.Since(lastEmit) >= 100*time.Millisecond {
				pct := 10 + int(float64(downloaded)/float64(resp.ContentLength)*80)
				if pct > 90 {
					pct = 90
				}
				runtime.EventsEmit(ctx, "core_update_progress", pct, fmt.Sprintf("Загрузка: %.1f / %.1f МБ", float64(downloaded)/(1024*1024), float64(resp.ContentLength)/(1024*1024)))
				lastEmit = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("ошибка при чтении потока: %w", readErr)
		}
	}
	return out.Bytes(), nil
}

func extractCoreExecutable(body []byte, sourceURL, goos, goarch string) ([]byte, error) {
	if int64(len(body)) > maxCoreDownloadBytes {
		return nil, fmt.Errorf("скачанный файл превышает лимит")
	}
	isZip := bytes.HasPrefix(body, []byte("PK\x03\x04"))
	if u, err := url.Parse(sourceURL); err == nil && strings.HasSuffix(strings.ToLower(u.Path), ".zip") {
		isZip = true
	}
	if !isZip {
		if int64(len(body)) > maxCoreExecutableBytes {
			return nil, fmt.Errorf("исполняемый файл превышает лимит %d bytes", maxCoreExecutableBytes)
		}
		return append([]byte(nil), body...), nil
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения zip-архива: %w", err)
	}
	allowed := zipCoreExecutableNames(goos, goarch)
	var candidate *zip.File
	for _, name := range allowed {
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || !strings.EqualFold(filepath.Base(f.Name), name) {
				continue
			}
			candidate = f
			break
		}
		if candidate != nil {
			break
		}
	}
	if candidate == nil {
		return nil, fmt.Errorf("в zip-архиве нет ожидаемого FreeTurn client binary")
	}
	if candidate.UncompressedSize64 > uint64(maxCoreExecutableBytes) {
		return nil, fmt.Errorf("файл %s в zip слишком большой", candidate.Name)
	}
	rc, err := candidate.Open()
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия %s: %w", candidate.Name, err)
	}
	defer rc.Close()
	data, err := readLimited(rc, maxCoreExecutableBytes)
	if err != nil {
		return nil, fmt.Errorf("распаковка %s: %w", candidate.Name, err)
	}
	return data, nil
}

func zipCoreExecutableNames(goos, goarch string) []string {
	names := expectedCoreAssetNames(goos, goarch)
	if goos == "windows" {
		return append(names, "freeturnclient.exe", "client.exe", "client-windows-amd64.exe")
	}
	return append(names, "freeturnclient", "client", "client-"+goos+"-"+goarch)
}

func validateExecutableBytes(data []byte, goos string) error {
	if len(data) < 1024 {
		return fmt.Errorf("исполняемый файл слишком мал или повреждён (%d байт)", len(data))
	}
	switch goos {
	case "windows":
		if !bytes.HasPrefix(data, []byte("MZ")) {
			return fmt.Errorf("файл не является Windows PE (нет MZ)")
		}
	case "linux":
		if !bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
			return fmt.Errorf("файл не является ELF")
		}
	case "darwin":
		if len(data) < 4 || !isMachOMagic(data[:4]) {
			return fmt.Errorf("файл не является Mach-O/FAT Mach-O")
		}
	}
	return nil
}

func isMachOMagic(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	magic := [4]byte{b[0], b[1], b[2], b[3]}
	switch magic {
	case [4]byte{0xcf, 0xfa, 0xed, 0xfe}, [4]byte{0xfe, 0xed, 0xfa, 0xcf},
		[4]byte{0xca, 0xfe, 0xba, 0xbe}, [4]byte{0xbe, 0xba, 0xfe, 0xca},
		[4]byte{0xca, 0xfe, 0xba, 0xbf}, [4]byte{0xbf, 0xba, 0xfe, 0xca}:
		return true
	default:
		return false
	}
}

func coreInstallPath() string {
	if p := getFreeturnPath(); p != "" {
		return p
	}
	exe, _ := os.Executable()
	name := "freeturnclient"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}

func replaceExecutableAtomically(targetPath string, data []byte) error {
	if err := validateExecutableBytes(data, goruntime.GOOS); err != nil {
		return err
	}
	if lfi, err := os.Lstat(targetPath); err == nil && lfi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("отказ от замены symlink: %s", targetPath)
	}
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("создание директории ядра: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".freeturn-update-*")
	if err != nil {
		return fmt.Errorf("создание временного файла ядра: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	backup := targetPath + ".old"
	_ = os.Remove(backup)
	existed := false
	if _, err := os.Stat(targetPath); err == nil {
		existed = true
		if err := os.Rename(targetPath, backup); err != nil {
			return fmt.Errorf("не удалось создать backup ядра: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		if existed {
			_ = os.Rename(backup, targetPath)
		}
		return fmt.Errorf("не удалось установить новое ядро: %w", err)
	}
	if err := os.Chmod(targetPath, 0o755); err != nil {
		if existed {
			_ = os.Remove(targetPath)
			_ = os.Rename(backup, targetPath)
		}
		return fmt.Errorf("chmod нового ядра: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("данные превышают лимит %d bytes", limit)
	}
	return data, nil
}

func SelectAndReplaceCore(ctx context.Context, beforeReplaceFn func()) (string, error) {
	pattern := "*"
	if goruntime.GOOS == "windows" {
		pattern = "*.exe"
	}
	selectedFile, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:   "Выберите исполняемый файл ядра FreeTurn",
		Filters: []runtime.FileFilter{{DisplayName: "Исполняемый файл", Pattern: pattern}},
	})
	if err != nil {
		return "", fmt.Errorf("ошибка открытия диалога: %w", err)
	}
	if selectedFile == "" {
		return "", nil
	}
	fi, err := os.Stat(selectedFile)
	if err != nil {
		return "", err
	}
	if fi.Size() > maxCoreExecutableBytes {
		return "", fmt.Errorf("выбранный файл слишком большой: %d bytes", fi.Size())
	}
	f, err := os.Open(selectedFile)
	if err != nil {
		return "", err
	}
	data, readErr := readLimited(f, maxCoreExecutableBytes)
	_ = f.Close()
	if readErr != nil {
		return "", readErr
	}
	if err := validateExecutableBytes(data, goruntime.GOOS); err != nil {
		return "", err
	}
	if beforeReplaceFn != nil {
		beforeReplaceFn()
	}
	targetPath := coreInstallPath()
	if err := replaceExecutableAtomically(targetPath, data); err != nil {
		return "", err
	}
	verFile := filepath.Join(filepath.Dir(targetPath), "core_version.txt")
	_ = os.Remove(verFile)
	newVersion := GetCoreVersion()
	_ = os.WriteFile(verFile, []byte(newVersion), 0o644)
	runtime.EventsEmit(ctx, "core_update_done", newVersion)
	log.Printf("[CoreUpdate] Ядро заменено вручную из %s. Версия: %s", selectedFile, newVersion)
	return newVersion, nil
}
