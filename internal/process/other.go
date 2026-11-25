//go:build !unix && !windows

package process

import "os/exec"

// Configure preserves CommandContext's direct child cancellation.
func Configure(c *exec.Cmd, detach bool) {}
