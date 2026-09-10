package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"acta2/internal/client"
	"acta2/internal/harnesspipe"
	"acta2/internal/hyperharness"
	"acta2/internal/providers"
	"github.com/spf13/cobra"
)

func (a *App) harnessCommand() *cobra.Command {
	var separate bool
	command := &cobra.Command{Use: "harness", Short: "Connect this machine to Acta until interrupted", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) (result error) {
		c, e := a.authenticatedClient()
		if e != nil {
			return e
		}
		hostname, e := os.Hostname()
		if e != nil {
			return fmt.Errorf("read hostname: %w", e)
		}
		report := func(s client.HarnessState) error {
			message := "Connected as " + s.Hostname + "."
			if s.State == "reconnecting" {
				message = fmt.Sprintf("Connection unavailable. Reconnecting in %.1fs…", s.RetryIn)
			}
			return a.emit(s, message)
		}
		a.notice("Connecting to " + c.URL + ". Press Ctrl+C to disconnect.")
		account, err := c.HarnessAccount(cmd.Context(), hostname, report)
		if err != nil {
			if cmd.Context().Err() != nil {
				return nil
			}
			return err
		}
		sum := sha256.Sum256([]byte(c.URL + "\n" + account.ID))
		stateDir := filepath.Join(a.repo.Dir, "harnesses", hex.EncodeToString(sum[:12]))
		pipeDir := filepath.Join(stateDir, "pipe")
		var pipe harnesspipe.API
		if separate {
			remote, err := a.detachedPipe(cmd.Context(), pipeDir)
			if err != nil {
				return err
			}
			defer remote.Close()
			pipe = remote
		} else {
			engine, err := harnesspipe.Open(pipeDir)
			if err != nil {
				return err
			}
			defer func() { result = errors.Join(result, engine.Close()) }()
			pipe = engine
		}
		runtime, err := hyperharness.NewController(cmd.Context(), filepath.Join(stateDir, "threads"), pipe)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, runtime.Close()) }()
		runtime.Recover()
		ctx, cancel := context.WithCancel(cmd.Context())
		discovery := providers.NewDiscovery()
		done := make(chan struct{})
		go func() { defer close(done); discovery.Run(ctx, providers.DefaultAdapters()) }()
		defer func() { cancel(); <-done }()
		err = c.RunHarness(ctx, hostname, discovery, runtime, report)
		if err == nil {
			return a.emit(client.HarnessState{State: "disconnected", Hostname: hostname}, "Disconnected.")
		}
		return err
	}}
	command.Flags().BoolVar(&separate, "separate-pipe", false, "Keep provider processes in a detached local pipe during harness restarts")
	return command
}

func (a *App) detachedPipe(ctx context.Context, dir string) (*harnesspipe.Remote, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	remote := harnesspipe.Connect(filepath.Join(dir, "pipe.sock"))
	if _, err := remote.Processes(ctx); err == nil {
		return remote, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(dir, "pipe.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	child := exec.Command(executable, "harness-pipe", "--directory", dir)
	child.Stdout = log
	child.Stderr = log
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "ACTA_TOKEN=") {
			child.Env = append(child.Env, v)
		}
	}
	harnesspipe.Detach(child)
	if err = child.Start(); err != nil {
		return nil, err
	}
	go func() { _ = child.Wait() }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if _, err = remote.Processes(ctx); err == nil {
			return remote, nil
		}
		select {
		case <-ctx.Done():
			remote.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			remote.Close()
			return nil, fmt.Errorf("detached pipe did not start; inspect %s", filepath.Join(dir, "pipe.log"))
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (a *App) pipeCommand() *cobra.Command {
	var dir string
	c := &cobra.Command{Use: "harness-pipe", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("pipe directory must be absolute")
		}
		return harnesspipe.Serve(cmd.Context(), dir)
	}}
	c.Flags().StringVar(&dir, "directory", "", "Private state directory")
	return c
}
