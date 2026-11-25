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
func Configure(c *exec.Cmd, detach bool) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: !detach, Setsid: detach}
	c.Cancel = func() error {
		e := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
}
