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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	_ = os.Chmod(dir, 0o700)

	if entries, err := os.ReadDir(dir); err == nil {
		var logs []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
				logs = append(logs, filepath.Join(dir, entry.Name()))
			}
		}
		if len(logs) >= 10 {
			for _, old := range logs[:len(logs)-9] {
				_ = os.Remove(old)
			}
		}
	}

	// Nanoseconds make separate user-initiated sessions unambiguous even when a
	// user disconnects/reconnects very quickly. Automatic reconnects reuse this
	// same file and therefore never fragment the failure history.
	name := time.Now().Format("2006-01-02_15-04-05.000000000") + "_" + sanitizeFilename(profileName) + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	_ = f.Chmod(0o600)
	return f
}

func (w *wailsLogWriter) start() {
	w.stop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
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
	batch := append([]logEntry(nil), w.buf...)
	w.buf = nil
	w.mu.Unlock()
	for _, entry := range batch {
		runtime.EventsEmit(w.ctx, "log", entry.level, entry.msg)
	}
}

func (w *wailsLogWriter) appendEntry(level, msg string) {
	level = strings.ToUpper(strings.TrimSpace(level))
	if level == "" {
		level = classifyLevel(msg)
	}
	msg = strings.TrimRight(msg, "\r\n")

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_, _ = fmt.Fprintf(w.file, "[%s] [%s] %s\n", time.Now().Format("15:04:05.000"), level, msg)
		// Errors are the most valuable lines when the process enters a reconnect
		// loop. Sync them immediately so a subsequent crash cannot lose them.
		if level == "ERROR" {
			_ = w.file.Sync()
		}
	}
	if len(w.buf) >= maxLogBuf {
		w.buf = w.buf[1:]
	}
	w.buf = append(w.buf, logEntry{level, msg})
}

func (w *wailsLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\r\n")
	if len(msg) > 20 && msg[4] == '/' && msg[7] == '/' && msg[10] == ' ' && msg[13] == ':' && msg[16] == ':' {
		msg = strings.TrimSpace(msg[20:])
	}
	w.appendEntry(classifyLevel(msg), msg)
	return len(p), nil
}

// emitSessionLog is the single path for session-visible logs. While a user
// connection session is active it stores the line in the same persistent file
// and queues it for Wails UI delivery. Outside a session it falls back to a
// normal Wails event.
func emitSessionLog(ctx context.Context, level, msg string) {
	if w, ok := log.Writer().(*wailsLogWriter); ok && w != nil {
		w.appendEntry(level, msg)
		return
	}
	runtime.EventsEmit(ctx, "log", level, msg)
}

func classifyLevel(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "fatal_auth") || strings.Contains(low, "ошибка") || strings.Contains(low, "error") || strings.Contains(low, "fatal") || strings.Contains(low, "фатальн"):
		return "ERROR"
	case strings.Contains(low, "warn") || strings.Contains(low, "не удалось") || strings.Contains(low, "повторим") || strings.Contains(low, "повторяем") || strings.Contains(low, "retry"):
		return "WARN"
	case strings.Contains(low, "debug") || strings.Contains(low, "obfs") || strings.Contains(low, "unwrap") || strings.Contains(low, "wrap:"):
		return "DEBUG"
	default:
		return "INFO"
	}
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.Getenv("HOME")
	}
	dir := filepath.Join(base, "FTurnPc-singbox")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
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

// ProfileData is the stored FreeTurn profile. Mode is the FreeTurn backend mode
// (tcp/udp) and is separate from Transport, which selects the TURN transport.
type ProfileData struct {
	Name           string          `json:"name"`
	Provider       string          `json:"provider"`
	PeerAddr       string          `json:"peer"`
	Transport      string          `json:"transport"`
	Mode           string          `json:"mode,omitempty"`
	Obf            string          `json:"obf"`
	Key            string          `json:"key"`
	Cid            string          `json:"cid"`
	WGConfig       string          `json:"wg"`
	Links          string          `json:"links,omitempty"`
	Power          int             `json:"power,omitempty"`
	StreamsPerCred int             `json:"streamsPerCred,omitempty"`
	SB             json.RawMessage `json:"sb,omitempty"`
}

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

type Orchestrator struct {
	appCtx context.Context

	mu            sync.Mutex
	transitionMu  sync.Mutex
	engine        *FreeturnEngine
	prevLogWriter io.Writer
	onTray        func(connected bool, rx, tx int64, workers int32)
	userStopped   bool
	lw            *wailsLogWriter
	wakeReconnect chan struct{}
	lastParams    ConnectParams
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
}

func NewOrchestrator(ctx context.Context, onTray func(bool, int64, int64, int32)) *Orchestrator {
	return &Orchestrator{appCtx: ctx, onTray: onTray, wakeReconnect: make(chan struct{}, 1)}
}

func (o *Orchestrator) triggerReconnect() {
	select {
	case o.wakeReconnect <- struct{}{}:
	default:
	}
}

func (o *Orchestrator) startSessionLog(profileName string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if _, already := log.Writer().(*wailsLogWriter); !already {
		o.prevLogWriter = log.Writer()
	}
	if o.lw != nil {
		o.stopLogWriterLocked()
	}
	o.lw = &wailsLogWriter{ctx: o.appCtx, file: newSessionLogFile(profileName)}
	o.lw.start()
	log.SetOutput(o.lw)
}

func (o *Orchestrator) Start(p ConnectParams) error {
	o.transitionMu.Lock()
	defer o.transitionMu.Unlock()

	o.mu.Lock()
	if o.engine != nil && o.engine.IsRunning() {
		o.mu.Unlock()
		emitSessionLog(o.appCtx, "ERROR", "FreeTurn уже запущен")
		return fmt.Errorf("already running")
	}
	oldEngine := o.engine
	o.engine = nil
	if o.sessionCancel != nil {
		o.sessionCancel()
	}
	o.userStopped = false
	o.lastParams = p
	parent := o.appCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	o.sessionCtx = ctx
	o.sessionCancel = cancel
	o.mu.Unlock()

	if oldEngine != nil {
		oldEngine.Stop()
	}
	// A new explicit Start is a new user session. Auto-reconnects below reuse
	// this writer until Stop or the next explicit Start.
	o.startSessionLog(p.Profile)
	emitSessionLog(o.appCtx, "INFO", fmt.Sprintf("===== SESSION START profile=%s =====", p.Profile))

	for {
		select {
		case <-o.wakeReconnect:
			continue
		default:
		}
		break
	}

	if err := o.startEngineOnly(ctx, p); err != nil {
		emitSessionLog(o.appCtx, "ERROR", fmt.Sprintf("Сессия не запущена: %v", err))
		cancel()
		o.stopLogWriter()
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

func (o *Orchestrator) startEngineOnly(ctx context.Context, p ConnectParams) error {
	emitSessionLog(o.appCtx, "INFO", fmt.Sprintf("Загрузка профиля: %s (path: %s)", p.Profile, profilePath(p.Profile)))
	prof, err := loadProfile(p.Profile)
	if err != nil {
		emitSessionLog(o.appCtx, "ERROR", fmt.Sprintf("Ошибка загрузки профиля: %v", err))
		return err
	}
	emitSessionLog(o.appCtx, "INFO", fmt.Sprintf("Профиль загружен: peer=%s transport=%s mode=%s obf=%s wg_len=%d", prof.PeerAddr, prof.Transport, prof.Mode, prof.Obf, len(prof.WGConfig)))

	engine := NewFreeturnEngine(ctx, o.onTray, func(err error) {
		emitSessionLog(o.appCtx, "WARN", fmt.Sprintf("[Auto-Reconnect] backend завершился: %v", err))
		o.triggerReconnect()
	})
	if err := engine.Start(p, prof); err != nil {
		emitSessionLog(o.appCtx, "ERROR", fmt.Sprintf("Ошибка запуска FreeTurn: %v", err))
		return err
	}

	o.mu.Lock()
	if o.userStopped || ctx.Err() != nil {
		o.mu.Unlock()
		engine.Stop()
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
	if o.prevLogWriter != nil {
		log.SetOutput(o.prevLogWriter)
	}
	lw := o.lw
	o.lw = nil
	if lw == nil {
		return
	}
	select {
	case <-lw.stop:
	default:
		close(lw.stop)
	}
	lw.mu.Lock()
	if lw.file != nil {
		_ = lw.file.Sync()
		_ = lw.file.Close()
		lw.file = nil
	}
	lw.mu.Unlock()
}

func (o *Orchestrator) Stop() {
	o.transitionMu.Lock()
	defer o.transitionMu.Unlock()

	o.mu.Lock()
	o.userStopped = true
	if o.sessionCancel != nil {
		o.sessionCancel()
	}
	engine := o.engine
	o.engine = nil
	o.mu.Unlock()

	if engine != nil {
		engine.Stop()
	}
	emitSessionLog(o.appCtx, "INFO", "===== SESSION STOP =====")
	o.stopLogWriter()
}

func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	engine := o.engine
	o.mu.Unlock()
	return engine != nil && engine.IsRunning()
}

func (o *Orchestrator) monitorNetwork(ctx context.Context, p ConnectParams) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-o.wakeReconnect:
		}

		o.mu.Lock()
		stopped := o.userStopped
		engine := o.engine
		o.mu.Unlock()
		if stopped || ctx.Err() != nil {
			return
		}
		engineRunning := engine != nil && engine.IsRunning()
		if !HasNetworkChanged() && engineRunning {
			continue
		}
		if !engineRunning {
			emitSessionLog(o.appCtx, "WARN", "[Auto-Reconnect] backend остановлен. Переподключение...")
		} else {
			emitSessionLog(o.appCtx, "WARN", "Смена сети. Переподключение...")
		}
		if !o.reconnect(ctx, p) {
			return
		}
	}
}

func (o *Orchestrator) reconnect(ctx context.Context, p ConnectParams) bool {
	o.transitionMu.Lock()
	defer o.transitionMu.Unlock()

	o.mu.Lock()
	if o.userStopped || ctx.Err() != nil {
		o.mu.Unlock()
		return false
	}
	engine := o.engine
	o.engine = nil
	o.mu.Unlock()
	if engine != nil {
		engine.Stop()
	}

	// Do not stop/recreate the log writer here. A reconnect is still the same
	// user session, so all attempts and their errors must remain in one file.
	attempt := 0
	for {
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}

		o.mu.Lock()
		stopped := o.userStopped
		o.mu.Unlock()
		if stopped {
			return false
		}
		if !IsInternetAvailable() {
			continue
		}
		attempt++
		emitSessionLog(o.appCtx, "INFO", fmt.Sprintf("[Auto-Reconnect] Попытка #%d: восстановление туннеля...", attempt))
		if err := o.startEngineOnly(ctx, p); err == nil {
			emitSessionLog(o.appCtx, "INFO", fmt.Sprintf("[Auto-Reconnect] Связь успешно восстановлена на попытке #%d", attempt))
			return true
		} else {
			emitSessionLog(o.appCtx, "WARN", fmt.Sprintf("[Auto-Reconnect] Попытка #%d не удалась: %v", attempt, err))
		}
	}
}
