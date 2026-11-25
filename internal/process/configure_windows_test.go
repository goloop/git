package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestWindowsConsoleMode(t *testing.T) {
	for _, detach := range []bool{true, false} {
		cmd := exec.CommandContext(t.Context(), "git", "version")
		const existingFlag = 0x00000200 // CREATE_NEW_PROCESS_GROUP
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: existingFlag}
		Configure(cmd, detach)
		flags := cmd.SysProcAttr.CreationFlags
		if flags&existingFlag == 0 || (flags&0x08000000 != 0) != detach {
			t.Fatalf("detach=%t: flags=%#x", detach, flags)
		}
		if output, err := cmd.Output(); err != nil || len(output) == 0 {
			t.Fatal(string(output), err)
		}
	}
}
