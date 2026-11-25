//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

// Configure starts unattended console programs without the parent's console.
// CommandContext retains responsibility for terminating the direct child.
func Configure(cmd *exec.Cmd, detach bool) {
	if detach {
		const createNoWindow = 0x08000000
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.CreationFlags |= createNoWindow
	}
}
