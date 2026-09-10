package providers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const ProbeTimeout = 5 * time.Second
const RefreshInterval = 30 * time.Second
const outputLimit = 32 * 1024

var errOutputLimit = errors.New("provider probe output limit exceeded")

type Result struct {
	Stdout, Stderr string
	Code           int
	Err            error
}
type Runner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, ...string) Result
}
type localRunner struct{}

func (localRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

type boundedOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > outputLimit {
		b.exceeded = true
		return 0, errOutputLimit
	}
	return b.buffer.Write(p)
}
func (localRunner) Run(ctx context.Context, path string, args ...string) Result {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// stdin is /dev/null. No shell, prompt, turn, or credential-file parsing.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "ACTA_TOKEN=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.WaitDelay = 250 * time.Millisecond
	configureProbe(cmd)
	err := cmd.Run()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		err = nil
	} // Exit status is represented by Code.
	if stdout.exceeded || stderr.exceeded {
		err = errOutputLimit
	}
	result := Result{Stdout: stdout.buffer.String(), Stderr: stderr.buffer.String(), Err: err, Code: -1}
	if cmd.ProcessState != nil {
		result.Code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		result.Err = ctx.Err()
	}
	return result
}
