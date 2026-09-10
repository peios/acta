package harnesspipe

import (
	"os/exec"
	"syscall"
)

func configureProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
func interruptProcess(c *exec.Cmd) error { return syscall.Kill(-c.Process.Pid, syscall.SIGINT) }
func forceProcess(c *exec.Cmd) error     { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }

func Detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// Signal zero only probes existence. A live (possibly reused) PID, permission
// error, or missing PID cannot prove that the original provider has exited.
func processAbsent(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == syscall.ESRCH
}
