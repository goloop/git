//go:build unix

// Package process configures cancellation of native Git commands.
package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Configure assigns a process group and kills the group on context cancellation.
func Configure(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		e := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
}
