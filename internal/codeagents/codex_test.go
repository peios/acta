//go:build unix

package codeagents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"acta/internal/codethreads"
)

func fakeCodex(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
}

const readCodexRequest = `IFS= read -r input
request_id=${input#*\"id\":\"}
request_id=${request_id%%\"*}
`
const acknowledgeCodex = `printf '{"id":"%s","result":{"userAgent":"test"}}\n' "$request_id"
`
const startCodexThread = readCodexRequest + acknowledgeCodex + `IFS= read -r initialized
` + readCodexRequest
const acknowledgeCodexThread = `printf '{"id":"%s","result":{"thread":{"id":"native-thread"}}}\n' "$request_id"
`

func TestCodexInitializesAndCreatesThreadWithoutTurn(t *testing.T) {
	fakeCodex(t, `printf '%s\n' "$@" "$PWD"
[ -z "$ACTA_TOKEN" ] || printf 'leaked Acta token\n'
printf 'diagnostic\n' >&2
`+readCodexRequest+`printf '%s\n' "$input"
`+acknowledgeCodex+`IFS= read -r initialized
printf '%s\n' "$initialized"
`+readCodexRequest+`printf '%s\n' "$input"
printf '{"method":"thread/started","params":{"thread":{"id":"native-thread"}}}\n'
`+acknowledgeCodexThread+`IFS= read -r extra
printf 'unexpected input or closed stdin\n'
`)
	t.Setenv("ACTA_TOKEN", "private-acta-token")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dir := t.TempDir()
	var mu sync.Mutex
	var frames []codethreads.Payload
	s, err := (Codex{}).Start(ctx, Launch{Directory: dir}, func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	if s.(*codexSession).threadID != "native-thread" {
		t.Fatal("native thread ID lost")
	}
	mu.Lock()
	var lines []string
	for _, f := range frames {
		if f.Type != "debug.unknown" || f.Provider != "codex" {
			t.Fatal("incorrect CAT mapping", f)
		}
		if f.Stream == "stdout" {
			lines = append(lines, f.Raw)
		}
	}
	mu.Unlock()
	if len(lines) != 10 || strings.Join(lines[:4], "\n") != "app-server\n--listen\nstdio://\n"+dir {
		t.Fatal("flags, cwd, or output lost", lines)
	}
	var init, initialized, start map[string]json.RawMessage
	if json.Unmarshal([]byte(lines[4]), &init) != nil || json.Unmarshal([]byte(lines[6]), &initialized) != nil || json.Unmarshal([]byte(lines[7]), &start) != nil {
		t.Fatal("invalid protocol input", lines)
	}
	if string(init["method"]) != `"initialize"` || string(initialized["method"]) != `"initialized"` || len(initialized) != 2 || string(start["method"]) != `"thread/start"` || string(init["id"]) == string(start["id"]) {
		t.Fatal("wrong handshake order or IDs", lines)
	}
	var initParams struct {
		ClientInfo struct {
			Name string `json:"name"`
		} `json:"clientInfo"`
	}
	if json.Unmarshal(init["params"], &initParams) != nil || initParams.ClientInfo.Name != "acta_code" {
		t.Fatal("client not identified")
	}
	var params map[string]string
	if json.Unmarshal(start["params"], &params) != nil || len(params) != 1 || params["cwd"] != dir {
		t.Fatal("unexpected thread overrides", params)
	}
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		t.Fatal("extra input sent or stdin closed", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Codex not reaped")
	}
}

func TestCodexStartupFailuresAndOutputDrain(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"initialize timeout", readCodexRequest + "exec /bin/sleep 10\n", "timed out"},
		{"wrong ID", readCodexRequest + `printf '{"id":"wrong","result":{}}\n'
exec /bin/sleep 10
`, "timed out"},
		{"stderr cannot acknowledge", readCodexRequest + `printf '{"id":"%s","result":{}}\n' "$request_id" >&2
exec /bin/sleep 10
`, "timed out"},
		{"server request cannot acknowledge", readCodexRequest + `printf '{"id":"%s","method":"approval","params":{}}\n' "$request_id"
exec /bin/sleep 10
`, "timed out"},
		{"initialize error", readCodexRequest + `printf '{"id":"%s","error":{"code":-1,"message":"rejected"}}\n' "$request_id"
exec /bin/sleep 10
`, "initialize failed"},
		{"invalid initialize result", readCodexRequest + `printf '{"id":"%s","result":null}\n' "$request_id"
exec /bin/sleep 10
`, "invalid initialize"},
		{"thread timeout", startCodexThread + "exec /bin/sleep 10\n", "timed out"},
		{"thread error", startCodexThread + `printf '{"id":"%s","error":{"code":-1,"message":"rejected"}}\n' "$request_id"
exec /bin/sleep 10
`, "thread/start failed"},
		{"missing thread ID", startCodexThread + acknowledgeCodex + "exec /bin/sleep 10\n", "usable thread ID"},
		{"early exit", readCodexRequest + "printf 'final partial diagnostic' >&2\n", "before startup"},
		{"overflow", readCodexRequest + "i=0; while [ $i -lt 7000 ]; do printf '0123456789'; i=$((i+1)); done\nexec /bin/sleep 10\n", "64 KiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeCodex(t, tc.script)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var mu sync.Mutex
			var frames []codethreads.Payload
			s, err := (Codex{initializeTimeout: 200 * time.Millisecond}).Start(ctx, Launch{Directory: t.TempDir()}, func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() })
			if s != nil || err == nil || !strings.Contains(err.Error(), tc.want) || ctx.Err() != nil {
				t.Fatal("incorrect startup failure", err)
			}
			if tc.name == "early exit" && (len(frames) != 1 || frames[0].Raw != "final partial diagnostic") {
				t.Fatal("final diagnostic lost", frames)
			}
		})
	}
}

func TestCodexAcknowledgementThenExit(t *testing.T) {
	fakeCodex(t, startCodexThread+acknowledgeCodexThread+"printf 'final partial diagnostic' >&2\n")
	var mu sync.Mutex
	var frames []codethreads.Payload
	s, err := (Codex{}).Start(t.Context(), Launch{Directory: t.TempDir()}, func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatal("output not drained", frames)
	}
}

func TestCodexStartupCancellation(t *testing.T) {
	fakeCodex(t, startCodexThread+"exec /bin/sleep 10\n")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	s, err := (Codex{}).Start(ctx, Launch{Directory: t.TempDir()}, func(codethreads.Payload) {})
	if s != nil || err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("startup cancellation not reaped", err)
	}
}
