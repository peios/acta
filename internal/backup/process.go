package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := (16 << 20) - b.Len()
	if room < len(p) {
		b.overflow = true
		p = p[:max(0, room)]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// Never invoke a shell. Kill the process group on timeout so grandchildren
// cannot continue writing after the service has recorded an interrupted job.
func command(ctx context.Context, executable string, args []string, input []byte) ([]byte, error) {
	return commandEnv(ctx, executable, args, input, nil)
}
func commandEnv(ctx context.Context, executable string, args []string, input []byte, env []string) ([]byte, error) {
	var out boundedOutput
	if err := commandOutput(ctx, executable, args, input, env, &out); err != nil {
		return nil, err
	}
	if out.overflow {
		return nil, errors.New("backup command output exceeded 16 MiB")
	}
	return out.Bytes(), nil
}
func commandOutput(ctx context.Context, executable string, args []string, input []byte, env []string, output io.Writer) error {
	c := exec.CommandContext(ctx, executable, args...)
	if env != nil {
		c.Env = append(os.Environ(), env...)
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 5 * time.Second
	c.Stdout = output
	// Provider configuration can contain secrets; stderr goes to the protected
	// operator log, never to browser responses or the public job record.
	c.Stderr = os.Stderr
	c.Stdin = bytes.NewReader(input)
	if err := c.Run(); err != nil {
		return errors.New("backup command failed; check the operator's pgBackRest log and configuration")
	}
	return nil
}
