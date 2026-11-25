//go:build !unix

package process

import "os/exec"

// Configure preserves CommandContext's direct child cancellation.
func Configure(c *exec.Cmd, detach bool) {}
