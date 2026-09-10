//go:build !linux

package harnesspipe

import (
	"os"
	"os/exec"
)

func configureProcess(c *exec.Cmd)       {}
func interruptProcess(c *exec.Cmd) error { return c.Process.Signal(os.Interrupt) }
func forceProcess(c *exec.Cmd) error     { return c.Process.Kill() }

func Detach(c *exec.Cmd) {}

// Keep recovery conservative on platforms without an absence probe.
func processAbsent(pid int) bool { return false }
