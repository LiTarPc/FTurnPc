//go:build !windows

package backend

import (
	"os/exec"
	"syscall"
)

func prepareProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachToJob(_ *exec.Cmd) {}

func signalStop(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// The child is started in its own process group; signal the whole group so
	// helper descendants cannot survive the parent.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}
