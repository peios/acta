package codeagents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"acta/internal/codethreads"
	"github.com/google/uuid"
)

type Codex struct{ initializeTimeout time.Duration }

// Codex uses one private stdio app-server process per Acta Code thread. The
// connection handshake alone is insufficient: Start waits for thread/start too.
func (c Codex) Start(parent context.Context, in Launch, emit func(codethreads.Payload)) (Session, error) {
	path, err := exec.LookPath("codex")
	if err != nil || !filepath.IsAbs(path) {
		return nil, errors.New("Codex was not found on the host PATH.")
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, path, "app-server", "--listen", "stdio://")
	cmd.Dir, cmd.WaitDelay = in.Directory, time.Second
	configureProcess(cmd)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "ACTA_TOKEN=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errors.New("Could not open Codex input.")
	}
	stdout, outWriter := io.Pipe()
	stderr, errWriter := io.Pipe()
	cmd.Stdout, cmd.Stderr = outWriter, errWriter
	if err = cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		stdout.Close()
		outWriter.Close()
		stderr.Close()
		errWriter.Close()
		return nil, errors.New("Could not start Codex in this folder.")
	}
	s := &codexSession{done: make(chan struct{})}
	s.messages = &messages{stdin: stdin, processDone: s.done, cancel: cancel, emit: emit, provider: "codex"}
	initializeID, threadID := uuid.NewString(), uuid.NewString()
	replies := map[string]chan codexResponse{initializeID: make(chan codexResponse, 1), threadID: make(chan codexResponse, 1)}
	readDone := make(chan error, 2)
	read := func(reader *io.PipeReader, stream string) {
		defer reader.Close()
		seen := make(map[string]bool)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), codethreads.MaxFrameBytes)
		for scanner.Scan() {
			emit(codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: stream, Raw: scanner.Text()})
			if stream == "stdout" {
				s.messages.observe(scanner.Bytes())
				var response codexResponse
				if json.Unmarshal(scanner.Bytes(), &response) == nil && response.Method == "" && replies[response.ID] != nil && !seen[response.ID] {
					seen[response.ID] = true
					replies[response.ID] <- response
				}
			}
		}
		if scanner.Err() != nil {
			cancel()
		}
		readDone <- scanner.Err()
	}
	go read(stdout, "stdout")
	go read(stderr, "stderr")
	go func() {
		err := cmd.Wait()
		outWriter.Close()
		errWriter.Close()
		stdin.Close()
		a, b := <-readDone, <-readDone
		cancel()
		if a != nil || b != nil {
			err = errors.New("Codex output could not be read or exceeded the 64 KiB frame limit.")
		} else if err != nil {
			err = errors.New("Codex exited unsuccessfully. Check its debug output.")
		}
		s.err = err
		close(s.done)
	}()
	// The startup worker owns stdin. Cancelling/reaping the process releases
	// blocked writes, and Start joins this worker before returning any failure.
	started := make(chan error, 1)
	go func() {
		encoder := json.NewEncoder(stdin)
		request := func(id, method string, params any) (json.RawMessage, error) {
			if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
				return nil, errors.New("Could not send Codex " + method + ".")
			}
			var response codexResponse
			select {
			case response = <-replies[id]:
			case <-s.done:
				// Readers have drained, including an acknowledgement followed by exit.
				select {
				case response = <-replies[id]:
				default:
					if s.err != nil {
						return nil, s.err
					}
					return nil, errors.New("Codex exited before startup completed.")
				}
			}
			if len(response.Error) > 0 && string(response.Error) != "null" {
				return nil, errors.New("Codex " + method + " failed. Check its debug output.")
			}
			var result map[string]json.RawMessage
			if json.Unmarshal(response.Result, &result) != nil || result == nil {
				return nil, errors.New("Codex returned an invalid " + method + " response.")
			}
			return response.Result, nil
		}
		_, err := request(initializeID, "initialize", map[string]any{"clientInfo": map[string]string{"name": "acta_code", "title": "Acta Code", "version": "0.1.0"}})
		if err == nil {
			err = encoder.Encode(map[string]any{"method": "initialized", "params": struct{}{}})
			if err != nil {
				err = errors.New("Could not acknowledge Codex initialization.")
			}
		}
		if err == nil {
			var result json.RawMessage
			// Use the user's Codex defaults. No model turn or permission override.
			result, err = request(threadID, "thread/start", map[string]string{"cwd": in.Directory})
			if err == nil {
				var payload struct {
					Thread struct {
						ID string `json:"id"`
					} `json:"thread"`
				}
				if json.Unmarshal(result, &payload) != nil || strings.TrimSpace(payload.Thread.ID) == "" {
					err = errors.New("Codex did not return a usable thread ID.")
				} else {
					s.threadID = payload.Thread.ID
				}
			}
		}
		started <- err
	}()
	timeout := c.initializeTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err = <-started:
		if err == nil {
			return s, nil
		}
	case <-timer.C:
		err = errors.New("Codex startup timed out.")
		cancel()
		<-started
	case <-parent.Done():
		err = parent.Err()
		cancel()
		<-started
	}
	cancel()
	_ = s.Wait()
	return nil, err
}

type codexResponse struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type codexSession struct {
	messages *messages
	done     chan struct{}
	err      error
	threadID string
}

func (s *codexSession) Wait() error { <-s.done; return s.err }
func (s *codexSession) Send(ctx context.Context, input codethreads.Input) error {
	return s.messages.send(ctx, input, s.threadID)
}
