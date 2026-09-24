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

type Claude struct{ initializeTimeout time.Duration }

func (c Claude) Start(parent context.Context, in Launch, emit func(codethreads.Payload)) (Session, error) {
	path, err := exec.LookPath("claude")
	if err != nil || !filepath.IsAbs(path) {
		return nil, errors.New("Claude Code was not found on the host PATH.")
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, path, "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages")
	cmd.Dir = in.Directory
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	// Provider auth/config stays inherited; Acta's own bearer override is not
	// a provider credential and need not be passed to the child.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "ACTA_TOKEN=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errors.New("Could not open Claude Code input.")
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
		return nil, errors.New("Could not start Claude Code in this folder.")
	}
	s := &claudeSession{done: make(chan struct{})}
	s.messages = &messages{stdin: stdin, processDone: s.done, cancel: cancel, emit: emit, provider: "claude"}
	requestID := uuid.NewString()
	initialized := make(chan error, 1)
	readDone := make(chan error, 2)
	read := func(reader *io.PipeReader, stream string) {
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), codethreads.MaxFrameBytes)
		for scanner.Scan() {
			emit(codethreads.Payload{Type: "debug.unknown", Provider: "claude", Stream: stream, Raw: scanner.Text()})
			// Preserve the raw frame first, even when the adapter understands this
			// response for its internal startup lifecycle. Stderr cannot acknowledge.
			if stream == "stdout" {
				s.messages.observe(scanner.Bytes())
				if matched, err := initializationResponse(scanner.Bytes(), requestID); matched {
					select {
					case initialized <- err:
					default:
					}
				}
			}
		}
		err := scanner.Err()
		if err != nil {
			cancel()
		}
		readDone <- err
	}
	go read(stdout, "stdout")
	go read(stderr, "stderr")
	go func() {
		err := cmd.Wait()
		// Wait completes the exec copying goroutines before closing our pipes,
		// so final output (including an unterminated last line) is retained.
		outWriter.Close()
		errWriter.Close()
		stdin.Close()
		a, b := <-readDone, <-readDone
		cancel()
		if a != nil || b != nil {
			err = errors.New("Claude Code output could not be read or exceeded the 64 KiB frame limit.")
		} else if err != nil {
			err = errors.New("Claude Code exited unsuccessfully. Check its debug output.")
		}
		s.err = err
		close(s.done)
	}()
	// Keep stdin open after initialization for explicit user submissions.
	timeout := c.initializeTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	written := make(chan struct{})
	var writeErr error
	go func() {
		writeErr = json.NewEncoder(stdin).Encode(struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}{Type: "control_request", RequestID: requestID, Request: struct {
			Subtype string `json:"subtype"`
		}{Subtype: "initialize"}})
		close(written)
	}()
	fail := func(err error) (Session, error) {
		cancel()
		_ = s.Wait()
		<-written
		return nil, err
	}
	exited := func() (Session, error) {
		// Readers have drained when done closes. If the provider acknowledged
		// and then exited, retain that distinction from a failed handshake.
		select {
		case err := <-initialized:
			if err != nil {
				return fail(err)
			}
			return s, nil
		default:
			return fail(startupExit(s.err))
		}
	}
	select {
	case <-written:
		if writeErr != nil {
			return fail(errors.New("Could not send Claude Code initialization."))
		}
	case <-timer.C:
		return fail(errors.New("Claude Code initialization timed out."))
	case <-parent.Done():
		return fail(parent.Err())
	case <-s.done:
		return exited()
	}
	select {
	case err := <-initialized:
		if err != nil {
			return fail(err)
		}
		return s, nil
	case <-timer.C:
		return fail(errors.New("Claude Code initialization timed out."))
	case <-parent.Done():
		return fail(parent.Err())
	case <-s.done:
		return exited()
	}
}

func initializationResponse(raw []byte, requestID string) (bool, error) {
	var envelope struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Type != "control_response" || envelope.Response.RequestID != requestID {
		return false, nil
	}
	if envelope.Response.Subtype == "success" {
		return true, nil
	}
	return true, errors.New("Claude Code initialization failed. Check its debug output.")
}

func startupExit(err error) error {
	if err != nil {
		return err
	}
	return errors.New("Claude Code exited before initialization completed.")
}

type claudeSession struct {
	messages *messages
	done     chan struct{}
	err      error
}

func (s *claudeSession) Wait() error { <-s.done; return s.err }
func (s *claudeSession) Send(ctx context.Context, input codethreads.Input) error {
	return s.messages.send(ctx, input, "")
}
