package codehost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"acta/internal/codehosts"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const providerInterval = time.Minute
const probeLimit = 64 * 1024

// Keep stdout/stderr bounded and local. Some status commands include account
// details or an API key fragment; none of their output is logged or relayed.
type probeOutput struct {
	data     []byte
	overflow bool
}

func (b *probeOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := probeLimit - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func probeAuth(ctx context.Context, path, dir, id string) string {
	args := []string{"login", "status"}
	if id == "claude" {
		args = []string{"auth", "status", "--json"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 250 * time.Millisecond
	var stdout, stderr probeOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil || stdout.overflow || stderr.overflow {
		return "unknown"
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "unknown"
		}
		code = exit.ExitCode()
	}
	return parseAuth(id, stdout.data, stderr.data, code)
}

func parseAuth(id string, stdout, stderr []byte, code int) string {
	if id == "claude" {
		var status struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if json.Unmarshal(stdout, &status) == nil && status.LoggedIn != nil {
			if *status.LoggedIn && code == 0 {
				return "signed_in"
			}
			if !*status.LoggedIn && code == 1 {
				return "signed_out"
			}
		}
	} else if id == "codex" {
		for _, line := range strings.Split(string(stdout)+"\n"+string(stderr), "\n") {
			line = strings.TrimSpace(line)
			if code == 0 && strings.HasPrefix(line, "Logged in using ") {
				return "signed_in"
			}
			if code == 1 && line == "Not logged in" {
				return "signed_out"
			}
		}
	}
	return "unknown"
}

func discoverProviders(ctx context.Context) codehosts.ProviderReport {
	report := codehosts.ProviderReport{Providers: []codehosts.ProviderStatus{
		{ID: "claude", Auth: "unknown"}, {ID: "codex", Auth: "unknown"},
	}}
	// Probe from the OS home, independent of any selected codebase or repository.
	home, homeErr := os.UserHomeDir()
	var wg sync.WaitGroup
	for i := range report.Providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := &report.Providers[i]
			path, err := exec.LookPath(p.ID)
			if err != nil || !filepath.IsAbs(path) {
				return
			}
			p.Installed = true
			if homeErr == nil && filepath.IsAbs(home) {
				p.Auth = probeAuth(ctx, path, home, p.ID)
			}
		}()
	}
	wg.Wait()
	return report
}

func publishProviders(ctx context.Context, conn *websocket.Conn, discover func(context.Context) codehosts.ProviderReport) {
	for {
		report := discover(ctx)
		if ctx.Err() != nil {
			return
		}
		write, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := wsjson.Write(write, conn, codehosts.Response{ProviderStatus: &report})
		cancel()
		if err != nil {
			return
		}
		timer := time.NewTimer(providerInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

var _ io.Writer = (*probeOutput)(nil)
