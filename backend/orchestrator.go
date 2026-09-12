package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsLogWriter перехватывает log.Printf и направляет в Wails-события.
type wailsLogWriter struct {
	ctx  context.Context
	mu   sync.Mutex
	buf  []logEntry
	stop chan struct{}
	file *os.File
}

const maxLogBuf = 500

type logEntry struct{ level, msg string }

func newSessionLogFile(profileName string) *os.File {
	dir := filepath.Join(configDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}

	// Simple log rotation: keep only the last 10 log files
	if entries, err := os.ReadDir(dir); err == nil {
		var logs []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".log") {
				logs = append(logs, filepath.Join(dir, e.Name()))
			}
		}
		if len(logs) >= 10 {
			for _, l := range logs[:len(logs)-9] {
				_ = os.Remove(l)
			}
		}
	}

	ts := time.Now().Format("2006-01-02_15-04-05")
	name := ts + "_" + sanitizeFilename(profileName) + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
}

func (w *wailsLogWriter) start() {
	w.stop = make(chan struct{})
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				w.flush()
			case <-w.stop:
				w.flush()
				return
			}
		}
	}()
}

func (w *wailsLogWriter) flush() {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return
	}
	batch := w.buf
	w.buf = nil
	w.mu.Unlock()
	for _, e := range batch {
		runtime.EventsEmit(w.ctx, "log", e.level, e.msg)
	}
}

func (w *wailsLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	// Стандартный префикс даты Go log: "YYYY/MM/DD HH:MM:SS "
	if len(msg) > 20 && msg[4] == '/' && msg[7] == '/' && msg[10] == ' ' && msg[13] == ':' && msg[16] == ':' {
		msg = strings.TrimSpace(msg[20:])
	}
	level := classifyLevel(msg)

	if w.file != nil {
		ts := time.Now().Format("15:04:05")
		fmt.Fprintf(w.file, "[%s] [%s] %s\n", ts, level, msg)
	}

	w.mu.Lock()
	if len(w.buf) >= maxLogBuf {
		w.buf = w.buf[1:]
	}
	w.buf = append(w.buf, logEntry{level, msg})
	w.mu.Unlock()
	return len(p), nil
}

func classifyLevel(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "fatal_auth") ||
		strings.Contains(low, "ошибка") ||
		strings.Contains(low, "error") ||
		strings.Contains(low, "fatal") ||
		strings.Contains(low, "фатальн"):
		return "ERROR"
	case strings.Contains(low, "warn") ||
		strings.Contains(low, "не удалось") ||
		strings.Contains(low, "повторим") ||
		strings.Contains(low, "повторяем") ||
		strings.Contains(low, "retry"):
		return "WARN"
	case strings.Contains(low, "debug") ||
		strings.Contains(low, "obfs") ||
		strings.Contains(low, "unwrap") ||
		strings.Contains(low, "wrap:"):
		return "DEBUG"
	default:
		return "INFO"
	}
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	dir := filepath.Join(base, "fturnpc")
	oldDir := filepath.Join(base, "pwdtt")
	if _, err := os.Stat(oldDir); err == nil {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			_ = os.Rename(oldDir, dir)
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func sanitizeFilename(name string) string {
	name = filepath.Base(filepath.Clean(name))
	if name == "." || name == "/" || name == "\\" {
		return "default"
	}
	return name
}

func profilePath(name string) string {
	return filepath.Join(configDir(), "profiles", sanitizeFilename(name)+".json")
}

// ProfileData — хранится в ~/.config/fturnpc/profiles/<name>.json
type ProfileData struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	PeerAddr  string `json:"peer"`
	Transport string `json:"transport"`
	Obf       string `json:"obf"`
	Key       string `json:"key"`
	Cid       string `json:"cid"`
	WGConfig       string `json:"wg"`
	Links          string `json:"links,omitempty"`
	Power          int    `json:"power,omitempty"`
	StreamsPerCred int    `json:"streamsPerCred,omitempty"`
}

// ConnectParams — runtime параметры от UI.
type ConnectParams struct {
	Profile  string `json:"profile"`
	Workers  int    `json:"workers,omitempty"`
	MTU      int    `json:"mtu,omitempty"`
	BypassRu bool   `json:"bypassRu,omitempty"`
}

func loadProfile(name string) (*ProfileData, error) {
	data, err := os.ReadFile(profilePath(name))
	if err != nil {
		return nil, fmt.Errorf("profile %q: %w", name, err)
	}
	var p ProfileData
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profile %q parse: %w", name, err)
	}
	return &p, nil
}

// Orchestrator — управляет процессом freeturnclient.exe
type Orchestrator struct {
	appCtx        context.Context
	mu            sync.Mutex
	engine        *FreeturnEngine
	prevLogWriter io.Writer
	onTray        func(connected bool, rx, tx int64, workers int32)
	userStopped   bool
	reconnectMu   sync.Mutex
	reconnecting  bool
	lw            *wailsLogWriter
	wakeReconnect chan struct{}
	lastParams    ConnectParams
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
}

func NewOrchestrator(ctx context.Context, onTray func(bool, int64, int64, int32)) *Orchestrator {
	return &Orchestrator{
		appCtx:        ctx,
		onTray:        onTray,
		wakeReconnect: make(chan struct{}, 1),
	}
}

func (o *Orchestrator) triggerReconnect() {
	select {
	case o.wakeReconnect <- struct{}{}:
	default:
	}
}

func (o *Orchestrator) Start(p ConnectParams) error {
	o.mu.Lock()
	if o.engine != nil && o.engine.IsRunning() {
		o.mu.Unlock()
		runtime.EventsEmit(o.appCtx, "log", "ERROR", "FreeTurn уже запущен")
		return fmt.Errorf("already running")
	}
	o.userStopped = false
	o.lastParams = p
	if o.sessionCancel != nil {
		o.sessionCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.sessionCtx = ctx
	o.sessionCancel = cancel
	o.mu.Unlock()

	if err := o.startEngineOnly(p); err != nil {
		return err
	}

	go o.monitorNetwork(ctx, p)
	return nil
}

func (o *Orchestrator) LastParams() ConnectParams {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastParams
}

func (o *Orchestrator) startEngineOnly(p ConnectParams) error {
	runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("Загрузка профиля: %s (path: %s)", p.Profile, profilePath(p.Profile)))
	prof, err := loadProfile(p.Profile)
	if err != nil {
		runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("Ошибка загрузки профиля: %v", err))
		return err
	}
	runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("Профиль загружен: peer=%s transport=%s obf=%s wg_len=%d", prof.PeerAddr, prof.Transport, prof.Obf, len(prof.WGConfig)))

	// Перехватываем стандартный логгер
	o.mu.Lock()
	if _, already := log.Writer().(*wailsLogWriter); !already {
		o.prevLogWriter = log.Writer()
	}
	if o.lw != nil {
		o.stopLogWriterLocked()
	}
	o.lw = &wailsLogWriter{ctx: o.appCtx, file: newSessionLogFile(p.Profile)}
	o.lw.start()
	log.SetOutput(o.lw)
	o.mu.Unlock()

	engine := NewFreeturnEngine(o.appCtx, o.onTray, func(err error) {
		o.triggerReconnect()
	})
	err = engine.Start(p, prof)
	if err != nil {
		runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("Ошибка запуска FreeTurn: %v", err))
		o.stopLogWriter()
		return err
	}

	o.mu.Lock()
	if o.userStopped {
		o.mu.Unlock()
		engine.Stop()
		o.stopLogWriter()
		return fmt.Errorf("stopped by user")
	}
	o.engine = engine
	o.mu.Unlock()
	return nil
}

func (o *Orchestrator) stopLogWriter() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopLogWriterLocked()
}

func (o *Orchestrator) stopLogWriterLocked() {
	if o.lw != nil {
		select {
		case <-o.lw.stop:
		default:
			close(o.lw.stop)
		}
		if o.lw.file != nil {
			o.lw.file.Close()
		}
		o.lw = nil
	}
	if o.prevLogWriter != nil {
		log.SetOutput(o.prevLogWriter)
	}
}

func (o *Orchestrator) Stop() {
	o.mu.Lock()
	o.userStopped = true
	if o.sessionCancel != nil {
		o.sessionCancel()
	}
	engine := o.engine
	o.mu.Unlock()
	
	if engine != nil {
		engine.Stop()
	}
	
	o.stopLogWriter()
}

func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.engine != nil && o.engine.IsRunning()
}

func (o *Orchestrator) monitorNetwork(ctx context.Context, p ConnectParams) {
	o.reconnectMu.Lock()
	if o.reconnecting {
		o.reconnectMu.Unlock()
		return
	}
	o.reconnecting = true
	o.reconnectMu.Unlock()
	
	defer func() {
		o.reconnectMu.Lock()
		o.reconnecting = false
		o.reconnectMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		case <-o.wakeReconnect:
		}

		o.mu.Lock()
		stopped := o.userStopped
		engineRunning := o.engine != nil && o.engine.IsRunning()
		o.mu.Unlock()
		if stopped {
			return
		}

		if HasNetworkChanged() || !engineRunning {
			if !engineRunning {
				runtime.EventsEmit(o.appCtx, "log", "WARN", "[Auto-Reconnect] Обнаружена остановка FreeTurn. Подготовка к переподключению...")
			} else {
				runtime.EventsEmit(o.appCtx, "log", "WARN", "Смена сети или потеря подключения. Подготовка к переподключению...")
			}
			
			o.mu.Lock()
			engine := o.engine
			o.mu.Unlock()
			if engine != nil {
				engine.Stop()
				o.stopLogWriter()
			}
			
			for {
				time.Sleep(3 * time.Second)
				o.mu.Lock()
				stopped = o.userStopped
				o.mu.Unlock()
				if stopped {
					return
				}

				if !IsInternetAvailable() {
					continue // wait until internet is actually available
				}

				runtime.EventsEmit(o.appCtx, "log", "INFO", "[Auto-Reconnect] Восстановление туннеля...")
				if err := o.startEngineOnly(p); err == nil {
					runtime.EventsEmit(o.appCtx, "log", "INFO", "[Auto-Reconnect] Связь успешно восстановлена!")
					break // exit inner reconnect loop, continue monitoring
				} else {
					runtime.EventsEmit(o.appCtx, "log", "WARN", fmt.Sprintf("[Auto-Reconnect] Ошибка восстановления: %v", err))
				}
			}
		}
	}
}
