//go:build !windows

package backend

import (
	"os"
	"os/exec"
	"syscall"
)

func prepareProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func attachToJob(_ *exec.Cmd) {}

func signalStop(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}
