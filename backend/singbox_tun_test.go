package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSingboxTunHelperProcess runs in a child copy of this test binary. This is
// a real OS process, so the lifecycle tests exercise exec.Cmd, pipes, Wait and
// cancellation instead of only feeding strings to parseLogs.
func TestSingboxTunHelperProcess(t *testing.T) {
	if os.Getenv("FTURN_SINGBOX_HELPER") != "1" {
		return
	}
	if marker := os.Getenv("FTURN_SINGBOX_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("started"), 0o600)
	}

	switch os.Getenv("FTURN_SINGBOX_SCENARIO") {
	case "ready-stderr-hold":
		fmt.Fprintln(os.Stderr, "INFO sing-box started (0.01s)")
		time.Sleep(30 * time.Second)
	case "error-stderr-hold":
		fmt.Fprintln(os.Stderr, "FATAL configure tun interface: access is denied")
		time.Sleep(30 * time.Second)
	case "fatal-exit":
		fmt.Fprintln(os.Stderr, "FATAL startup failed")
		os.Exit(2)
	case "ready-stdout-exit":
		fmt.Fprintln(os.Stdout, "INFO sing-box started (0.01s)")
		time.Sleep(80 * time.Millisecond)
		os.Exit(0)
	case "ready-stdout-hold":
		fmt.Fprintln(os.Stdout, "INFO sing-box started (0.01s)")
		time.Sleep(30 * time.Second)
	case "silent-hold":
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper scenario")
		os.Exit(3)
	}
}

func TestSingboxTunStart_CheckFailureDoesNotSpawn(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-hold", "")
	spawned := false
	singboxCheckFunc = func(string, string) error { return errors.New("invalid config") }
	singboxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		spawned = true
		return sbHelperCommand(ctx, "ready-stdout-hold", "")
	}

	tun := &SingboxTun{}
	err := tun.Start("broken.json")
	if err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("Start error = %v, want config-check failure", err)
	}
	if spawned {
		t.Fatal("sing-box process was spawned after config check failure")
	}
}

func TestSingboxTunStart_ReadyOnStderrWhileStdoutOpen(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stderr-hold", "")
	tun := &SingboxTun{}
	if err := tun.Start("config.json"); err != nil {
		t.Fatalf("Start failed although helper emitted readiness on stderr: %v", err)
	}
	defer tun.Stop()
	if !tun.IsRunning() {
		t.Fatal("IsRunning() = false after successful readiness")
	}
}

func TestSingboxTunStart_ErrorOnStderrReturnsPromptly(t *testing.T) {
	installSingboxLifecycleGlobals(t, "error-stderr-hold", "")
	singboxReadyTimeout = 800 * time.Millisecond
	singboxStopTimeout = 50 * time.Millisecond

	tun := &SingboxTun{}
	started := time.Now()
	err := tun.Start("config.json")
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("Start succeeded after fatal startup log")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "access is denied") {
		t.Fatalf("Start error = %q, want original fatal stderr line", err)
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("fatal stderr was not handled promptly; Start returned after %s", elapsed)
	}
}

func TestSingboxTunStart_EarlyExitIsAlwaysWaited(t *testing.T) {
	installSingboxLifecycleGlobals(t, "fatal-exit", "")
	var captured *exec.Cmd
	singboxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		captured = sbHelperCommand(ctx, "fatal-exit", "")
		return captured
	}

	tun := &SingboxTun{}
	if err := tun.Start("config.json"); err == nil {
		t.Fatal("Start succeeded although helper exited during startup")
	}
	if captured == nil {
		t.Fatal("helper command was not created")
	}
	if captured.ProcessState == nil {
		t.Fatal("child exited before readiness but cmd.Wait was never completed")
	}
}

func TestSingboxTunStop_CancelsInProgressStartPromptly(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "helper-started")
	installSingboxLifecycleGlobals(t, "silent-hold", marker)
	singboxReadyTimeout = 2 * time.Second

	tun := &SingboxTun{}
	startDone := make(chan error, 1)
	go func() { startDone <- tun.Start("config.json") }()
	sbWaitForFile(t, marker, time.Second)

	stopDone := make(chan struct{})
	go func() {
		tun.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Stop blocked behind an in-progress Start")
	}
	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("Start returned nil after Stop cancelled startup")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start did not terminate after Stop")
	}
}

func TestSingboxTunStop_DuringConfigCheckNeverSpawnsChild(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-hold", "")
	checkEntered := make(chan struct{})
	releaseCheck := make(chan struct{})
	var spawned atomic.Bool
	singboxCheckFunc = func(string, string) error {
		close(checkEntered)
		<-releaseCheck
		return nil
	}
	singboxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		spawned.Store(true)
		return sbHelperCommand(ctx, "ready-stdout-hold", "")
	}

	tun := &SingboxTun{}
	startDone := make(chan error, 1)
	go func() { startDone <- tun.Start("config.json") }()
	<-checkEntered
	tun.Stop()
	close(releaseCheck)

	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("Start succeeded after Stop cancelled it during config check")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start remained stuck after config check returned")
	}
	if spawned.Load() {
		t.Fatal("child was spawned after startup had already been cancelled")
	}
}

func TestSingboxTunStop_SecondStartRejectedWhileFirstCheckUnwinds(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-hold", "")
	checkEntered := make(chan struct{})
	releaseCheck := make(chan struct{})
	singboxCheckFunc = func(string, string) error {
		close(checkEntered)
		<-releaseCheck
		return nil
	}

	tun := &SingboxTun{}
	startDone := make(chan error, 1)
	go func() { startDone <- tun.Start("config.json") }()
	<-checkEntered
	tun.Stop()
	if err := tun.Start("config2.json"); err == nil {
		t.Fatal("second Start was accepted while the cancelled first Start was still unwinding")
	}
	close(releaseCheck)
	<-startDone
}

func TestSingboxTun_UnexpectedExitClearsStateAndNotifies(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-exit", "")
	exitCh := make(chan error, 1)
	tun := &SingboxTun{onUnexpectedExit: func(err error) { exitCh <- err }}
	if err := tun.Start("config.json"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !tun.IsRunning() {
		t.Fatal("IsRunning() = false immediately after successful Start")
	}

	select {
	case err := <-exitCh:
		if err == nil {
			t.Fatal("unexpected-exit callback received nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unexpected process exit did not trigger callback")
	}
	if tun.IsRunning() {
		t.Fatal("IsRunning() stayed true after child exited")
	}
}

func TestSingboxTunStart_SecondStartRejected(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-hold", "")
	tun := &SingboxTun{}
	if err := tun.Start("config.json"); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer tun.Stop()
	if err := tun.Start("config.json"); err == nil {
		t.Fatal("second Start succeeded while sing-box was already running")
	}
}

func TestSingboxTunStart_UsesExpectedRunArguments(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-hold", "")
	var gotName string
	var gotArgs []string
	singboxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return sbHelperCommand(ctx, "ready-stdout-hold", "")
	}

	tun := &SingboxTun{}
	if err := tun.Start("C:/tmp/fturn-config.json"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()
	if gotName != "test-sing-box" {
		t.Fatalf("command name = %q, want injected sing-box path", gotName)
	}
	want := []string{"run", "-c", "C:/tmp/fturn-config.json", "--disable-color"}
	if strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("run args = %#v, want %#v", gotArgs, want)
	}
}

func TestSingboxTun_AppContextCancellationStopsChildWithoutUnexpectedCallback(t *testing.T) {
	installSingboxLifecycleGlobals(t, "ready-stdout-hold", "")
	ctx, cancel := context.WithCancel(context.Background())
	callback := make(chan error, 1)
	tun := &SingboxTun{appCtx: ctx, onUnexpectedExit: func(err error) { callback <- err }}
	if err := tun.Start("config.json"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()

	deadline := time.Now().Add(700 * time.Millisecond)
	for tun.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tun.IsRunning() {
		t.Fatal("sing-box child survived app/session context cancellation")
	}
	select {
	case err := <-callback:
		t.Fatalf("intentional parent-context cancellation was reported as unexpected exit: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestSingboxParseLogs_LongLineStillFindsReadiness(t *testing.T) {
	tun := &SingboxTun{}
	readyCh := make(chan struct{}, 1)
	errCh := make(chan string, 1)
	var readySeen atomic.Bool
	var readyOnce sync.Once
	input := strings.Repeat("x", 256*1024) + "\nINFO sing-box started (0.01s)\n"
	tun.parseLogs(strings.NewReader(input), &readySeen, &readyOnce, readyCh, errCh)
	select {
	case <-readyCh:
	default:
		t.Fatal("readiness after a 256 KiB log line was lost")
	}
}

func installSingboxLifecycleGlobals(t *testing.T, scenario, marker string) {
	t.Helper()
	oldPath := singboxPathFunc
	oldCheck := singboxCheckFunc
	oldVersion := singboxVersionCheckFunc
	oldCommand := singboxCommandContext
	oldSignal := singboxSignalStopFunc
	oldPrepare := singboxPrepareFunc
	oldAttach := singboxAttachFunc
	oldReadyTimeout := singboxReadyTimeout
	oldStopTimeout := singboxStopTimeout
	t.Cleanup(func() {
		singboxPathFunc = oldPath
		singboxCheckFunc = oldCheck
		singboxVersionCheckFunc = oldVersion
		singboxCommandContext = oldCommand
		singboxSignalStopFunc = oldSignal
		singboxPrepareFunc = oldPrepare
		singboxAttachFunc = oldAttach
		singboxReadyTimeout = oldReadyTimeout
		singboxStopTimeout = oldStopTimeout
	})

	singboxPathFunc = func() string { return "test-sing-box" }
	singboxVersionCheckFunc = func(string) error { return nil }
	singboxCheckFunc = func(string, string) error { return nil }
	singboxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return sbHelperCommand(ctx, scenario, marker)
	}
	singboxPrepareFunc = func(*exec.Cmd) {}
	singboxAttachFunc = func(*exec.Cmd) {}
	singboxSignalStopFunc = func(cmd *exec.Cmd) error {
		if cmd == nil || cmd.Process == nil {
			return errors.New("process is not running")
		}
		return cmd.Process.Kill()
	}
	singboxReadyTimeout = 500 * time.Millisecond
	singboxStopTimeout = 100 * time.Millisecond
}

func sbHelperCommand(ctx context.Context, scenario, marker string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSingboxTunHelperProcess$", "--")
	cmd.Env = append(os.Environ(),
		"FTURN_SINGBOX_HELPER=1",
		"FTURN_SINGBOX_SCENARIO="+scenario,
		"FTURN_SINGBOX_MARKER="+marker,
	)
	return cmd
}

func sbWaitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("helper did not start within %s: %v", timeout, err)
	}
}
