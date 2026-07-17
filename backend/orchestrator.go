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

func newSessionLogFile(peerIP string) *os.File {
	dir := filepath.Join(configDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	ts := time.Now().Format("2006-01-02_15-04-05")
	name := ts + "_" + peerIP + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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
	if len(msg) > 20 && msg[4] == '/' {
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
	dir := filepath.Join(base, "pwdtt")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func profilePath(name string) string {
	return filepath.Join(configDir(), "profiles", name+".json")
}

// ProfileData — хранится в ~/.config/pwdtt/profiles/<name>.json
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
	Profile string `json:"profile"`
	Workers int    `json:"workers,omitempty"`
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
	lw            *wailsLogWriter
}

func NewOrchestrator(ctx context.Context, onTray func(bool, int64, int64, int32)) *Orchestrator {
	return &Orchestrator{appCtx: ctx, onTray: onTray}
}

func (o *Orchestrator) Start(p ConnectParams) error {
	o.mu.Lock()
	if o.engine != nil && o.engine.IsRunning() {
		o.mu.Unlock()
		runtime.EventsEmit(o.appCtx, "log", "ERROR", "FreeTurn уже запущен")
		return fmt.Errorf("already running")
	}
	o.mu.Unlock()

	runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("Загрузка профиля: %s (path: %s)", p.Profile, profilePath(p.Profile)))
	prof, err := loadProfile(p.Profile)
	if err != nil {
		runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("Ошибка загрузки профиля: %v", err))
		return err
	}
	runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("Профиль загружен: peer=%s transport=%s obf=%s wg_len=%d", prof.PeerAddr, prof.Transport, prof.Obf, len(prof.WGConfig)))

	// Перехватываем стандартный логгер
	if _, already := log.Writer().(*wailsLogWriter); !already {
		o.prevLogWriter = log.Writer()
	}
	o.lw = &wailsLogWriter{ctx: o.appCtx, file: newSessionLogFile(p.Profile)}
	o.lw.start()
	log.SetOutput(o.lw)

	engine := NewFreeturnEngine(o.appCtx, o.onTray)
	err = engine.Start(p, prof)
	if err != nil {
		runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("Ошибка запуска FreeTurn: %v", err))
		o.stopLogWriter()
		return err
	}

	o.mu.Lock()
	o.engine = engine
	o.mu.Unlock()
	return nil
}

func (o *Orchestrator) stopLogWriter() {
	if lw, ok := log.Writer().(*wailsLogWriter); ok {
		select {
		case <-lw.stop:
		default:
			close(lw.stop)
		}
		if lw.file != nil {
			lw.file.Close()
		}
	}
	if o.prevLogWriter != nil {
		log.SetOutput(o.prevLogWriter)
	}
}

func (o *Orchestrator) Stop() {
	o.mu.Lock()
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
