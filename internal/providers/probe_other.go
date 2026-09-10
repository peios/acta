//go:build !unix

package providers

import "os/exec"

// CommandContext kills the probe; WaitDelay also bounds inherited output pipes.
func configureProbe(cmd *exec.Cmd) {}
