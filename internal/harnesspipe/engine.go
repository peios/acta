package harnesspipe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"acta2/internal/localstate"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

type persisted struct {
	Process Process           `json:"process"`
	Writes  map[string]string `json:"writes"`
}
type child struct {
	data    persisted
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	journal *Journal
}
type Engine struct {
	mu       sync.Mutex
	dir      string
	lock     *flock.Flock
	children map[string]*child
	journals map[string]*Journal
	closed   bool
}

func Open(dir string) (*Engine, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("pipe state directory must be private")
	}
	lock := flock.New(filepath.Join(dir, "owner.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("another process owns this pipe")
	}
	e := &Engine{dir: dir, lock: lock, children: map[string]*child{}, journals: map[string]*Journal{}}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		lock.Close()
		return nil, err
	}
	for _, path := range files {
		var data persisted
		if err = localstate.Read(path, &data); err != nil {
			e.Close()
			return nil, err
		}
		if _, err = uuid.Parse(data.Process.Spec.RunID); err != nil {
			e.Close()
			return nil, err
		}
		if data.Process.State != "exited" {
			data.Process.State = "uncertain"
			data.Process.Error = "Pipe exited before recording a final process outcome; automatic relaunch is disabled."
			if processAbsent(data.Process.PID) {
				data.Process.State = "exited"
				data.Process.Error = "Provider process no longer exists after pipe recovery; its final outcome is unknown."
				if err = localstate.Write(path, data); err != nil {
					e.Close()
					return nil, err
				}
			}
		}
		c := &child{data: data, done: make(chan struct{})}
		close(c.done)
		e.children[data.Process.Spec.RunID] = c
	}
	return e, nil
}
func (e *Engine) save(c *child) error {
	return localstate.Write(filepath.Join(e.dir, c.data.Process.Spec.RunID+".json"), c.data)
}
func (e *Engine) journal(thread string) (*Journal, error) {
	if _, err := uuid.Parse(thread); err != nil {
		return nil, errors.New("invalid thread id")
	}
	if j := e.journals[thread]; j != nil {
		return j, nil
	}
	j, err := openJournal(filepath.Join(e.dir, thread+".frames"))
	if err == nil {
		e.journals[thread] = j
	}
	return j, err
}
func (e *Engine) Spawn(ctx context.Context, s Spec) (Process, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Process{}, errors.New("pipe is closed")
	}
	if _, err := uuid.Parse(s.RunID); err != nil {
		return Process{}, errors.New("invalid run id")
	}
	if c := e.children[s.RunID]; c != nil {
		if !reflect.DeepEqual(c.data.Process.Spec, s) {
			return Process{}, errors.New("run id reused with different spawn parameters")
		}
		return c.data.Process, nil
	}
	for _, c := range e.children {
		if c.data.Process.Spec.ThreadID == s.ThreadID && c.data.Process.State != "exited" {
			return Process{}, errors.New("thread already has a running or uncertain process")
		}
	}
	if !filepath.IsAbs(s.CWD) || !filepath.IsAbs(s.Executable) {
		return Process{}, errors.New("provider paths must be absolute")
	}
	info, err := os.Stat(s.CWD)
	if err != nil || !info.IsDir() {
		return Process{}, errors.New("working directory does not exist")
	}
	if err = ctx.Err(); err != nil {
		return Process{}, err
	}
	j, err := e.journal(s.ThreadID)
	if err != nil {
		return Process{}, err
	}
	c := &child{data: persisted{Process: Process{Spec: s, State: "starting"}, Writes: map[string]string{}}, done: make(chan struct{}), journal: j}
	if err = e.save(c); err != nil {
		return Process{}, err
	} // Durable spawn intent precedes the OS side effect.
	e.children[s.RunID] = c
	cmd := exec.Command(s.Executable, s.Args...)
	cmd.Dir = s.CWD
	configureProcess(cmd)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "ACTA_TOKEN=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return e.spawnFailed(c, err)
	}
	stdout, outWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		return e.spawnFailed(c, err)
	}
	stderr, errWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		outWriter.Close()
		return e.spawnFailed(c, err)
	}
	cmd.Stdout = outWriter
	cmd.Stderr = errWriter
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		startErr := cmd.Start()
		started <- startErr
		if startErr == nil {
			<-c.done
		}
	}()
	err = <-started
	outWriter.Close()
	errWriter.Close()
	if err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return e.spawnFailed(c, err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.data.Process.PID = cmd.Process.Pid
	c.data.Process.State = "running"
	if err = e.save(c); err != nil {
		_ = forceProcess(cmd)
		_ = cmd.Wait()
		stdin.Close()
		stdout.Close()
		stderr.Close()
		close(c.done)
		c.data.Process.State = "uncertain"
		return c.data.Process, err
	}
	go e.capture(c, stdout, stderr)
	return c.data.Process, nil
}
func (e *Engine) spawnFailed(c *child, err error) (Process, error) {
	c.data.Process.State = "exited"
	c.data.Process.Error = err.Error()
	close(c.done)
	if save := e.save(c); save != nil {
		return c.data.Process, save
	}
	return c.data.Process, err
}
func (e *Engine) capture(c *child, stdout, stderr io.ReadCloser) {
	var readers sync.WaitGroup
	var captureMu sync.Mutex
	var captureErr error
	for _, stream := range []struct {
		name string
		r    io.ReadCloser
	}{{"stdout", stdout}, {"stderr", stderr}} {
		readers.Go(func() {
			defer stream.r.Close()
			scanner := bufio.NewScanner(stream.r)
			scanner.Buffer(make([]byte, 4096), MaxFrame)
			fail := func(err error) {
				captureMu.Lock()
				if captureErr == nil {
					captureErr = err
				}
				captureMu.Unlock()
				_ = forceProcess(c.cmd)
			}
			for scanner.Scan() {
				raw := append([]byte(nil), scanner.Bytes()...)
				if stream.name == "stderr" {
					raw, _ = json.Marshal(string(raw))
				} else if !json.Valid(raw) {
					fail(errors.New("provider wrote invalid JSON on stdout"))
					return
				}
				if err := c.journal.Append(c.data.Process.Spec.RunID, stream.name, raw); err != nil {
					fail(err)
					return
				}
			}
			if err := scanner.Err(); err != nil {
				fail(err)
			}
		})
	}
	err := c.cmd.Wait()
	_ = forceProcess(c.cmd) // A provider exit must not leave descendants retaining the pipes.
	readers.Wait()
	e.mu.Lock()
	defer e.mu.Unlock()
	c.data.Process.State = "exited"
	if captureErr != nil {
		c.data.Process.Error = "Provider capture failed: " + captureErr.Error()
	} else if err != nil {
		c.data.Process.Error = err.Error()
	}
	if err = e.save(c); err != nil {
		c.data.Process.State = "uncertain"
		c.data.Process.Error = "Could not persist process exit: " + err.Error()
	}
	close(c.done)
}
func (e *Engine) Write(ctx context.Context, r WriteRequest) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.children[r.RunID]
	if c == nil {
		return errors.New("unknown provider run")
	}
	if old, ok := c.data.Writes[r.ID]; ok {
		if old == r.Data {
			return nil
		}
		return errors.New("provider input has an uncertain outcome or command id conflict")
	}
	if len(r.Data) > MaxInput || r.ID == "" {
		return errors.New("invalid provider input")
	}
	if c.data.Process.State != "running" || c.stdin == nil {
		return errors.New("provider is not running")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.data.Writes[r.ID] = ""
	if err := e.save(c); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { _, err := io.WriteString(c.stdin, r.Data); done <- err }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		_ = forceProcess(c.cmd)
		<-done
		return ctx.Err()
	case <-timer.C:
		_ = forceProcess(c.cmd)
		<-done
		return errors.New("provider input timed out; acceptance is uncertain")
	}
	c.data.Writes[r.ID] = r.Data
	if err := e.save(c); err != nil {
		c.data.Writes[r.ID] = ""
		return err
	}
	return nil
}
func (e *Engine) Kill(ctx context.Context, run string) error {
	e.mu.Lock()
	c := e.children[run]
	if c == nil {
		e.mu.Unlock()
		return errors.New("unknown provider run")
	}
	if c.data.Process.State == "exited" {
		e.mu.Unlock()
		return nil
	}
	if c.cmd == nil {
		e.mu.Unlock()
		return errors.New("process ownership is uncertain; refusing to signal a saved PID")
	}
	done := c.done
	select {
	case <-done:
		e.mu.Unlock()
		return errors.New("provider exited but its final outcome is uncertain")
	default:
	}
	cmd := c.cmd
	e.mu.Unlock()
	outcome := func() error {
		e.mu.Lock()
		defer e.mu.Unlock()
		if c.data.Process.State != "exited" {
			return errors.New(c.data.Process.Error)
		}
		return nil
	}
	_ = interruptProcess(cmd)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return outcome()
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	_ = forceProcess(cmd)
	select {
	case <-done:
		return outcome()
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New("provider did not exit after forced termination")
	}
}
func (e *Engine) Processes(context.Context) ([]Process, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Process{}
	for _, c := range e.children {
		p := c.data.Process
		p.Spec.Args = append([]string(nil), p.Spec.Args...)
		out = append(out, p)
	}
	return out, nil
}
func (e *Engine) Read(_ context.Context, r ReadRequest) ([]RawFrame, error) {
	e.mu.Lock()
	j, err := e.journal(r.ThreadID)
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return j.Read(r.After)
}
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	var runs []string
	for id, c := range e.children {
		if c.cmd != nil {
			runs = append(runs, id)
		}
	}
	e.mu.Unlock()
	var result error
	for _, id := range runs {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		err := e.Kill(ctx, id)
		cancel()
		result = errors.Join(result, err)
	}
	if result != nil {
		return fmt.Errorf("pipe shutdown: %w", result)
	}
	for _, j := range e.journals {
		result = errors.Join(result, j.Close())
	}
	return errors.Join(result, e.lock.Close())
}
