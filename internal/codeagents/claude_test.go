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

func fakeClaude(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
}

const readInitialization = `IFS= read -r input
request_id=${input#*\"request_id\":\"}
request_id=${request_id%%\"*}
`
const acknowledgeInitialization = `printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s","response":{}}}\n' "$request_id"
`

func TestClaudeInitializesBeforeReadyAndSendsNoPrompt(t *testing.T) {
	fakeClaude(t, `printf '%s\n' "$@" "$PWD"
printf '{"type":"system","subtype":"hook_started"}\n'
printf 'diagnostic\n' >&2
[ -z "$ACTA_TOKEN" ] || printf 'leaked Acta token\n'
`+readInitialization+`printf '%s\n' "$input"
`+acknowledgeInitialization+`IFS= read -r extra
printf 'unexpected input\n'
`)
	t.Setenv("ACTA_TOKEN", "private-acta-credential")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dir := t.TempDir()
	var mu sync.Mutex
	var frames []codethreads.Payload
	s, err := (Claude{}).Start(ctx, Launch{Directory: dir}, func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	var stdout []string
	for _, frame := range frames {
		if frame.Type != "debug.unknown" || frame.Provider != "claude" {
			t.Fatal("incorrect CAT mapping")
		}
		if frame.Stream == "stdout" {
			stdout = append(stdout, frame.Raw)
		}
	}
	mu.Unlock()
	want := "--print\n--input-format\nstream-json\n--output-format\nstream-json\n--verbose\n--replay-user-messages\n" + dir + "\n" + `{"type":"system","subtype":"hook_started"}`
	if len(stdout) != 11 || strings.Join(stdout[:9], "\n") != want {
		t.Fatal("flags, cwd or early hook lost", stdout)
	}
	var request struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if json.Unmarshal([]byte(stdout[9]), &request) != nil || request.Type != "control_request" || request.Request.Subtype != "initialize" || request.RequestID == "" {
		t.Fatal("incorrect initialization request", stdout[9])
	}
	if matched, err := initializationResponse([]byte(stdout[10]), request.RequestID); !matched || err != nil {
		t.Fatal("ready before matching response")
	}
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		t.Fatal("stdin closed or extra input sent", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process not reaped")
	}
}

func TestClaudeInitializationFailuresAndOutputDrain(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"timeout", readInitialization + "exec /bin/sleep 10\n", "timed out"},
		{"wrong ID", readInitialization + `printf '{"type":"control_response","response":{"subtype":"success","request_id":"other"}}\n'
exec /bin/sleep 10
`, "timed out"},
		{"stderr cannot acknowledge", readInitialization + `printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s"}}\n' "$request_id" >&2
exec /bin/sleep 10
`, "timed out"},
		{"rejection", readInitialization + `printf '{"type":"control_response","response":{"subtype":"error","request_id":"%s","error":"rejected"}}\n' "$request_id"
exec /bin/sleep 10
`, "initialization failed"},
		{"early exit", "printf 'last diagnostic' >&2\nexit 0\n", "initialization"},
		{"overflow", "i=0; while [ $i -lt 7000 ]; do printf '0123456789'; i=$((i+1)); done\nexec /bin/sleep 10\n", "64 KiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClaude(t, tc.script)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var mu sync.Mutex
			var frames []codethreads.Payload
			s, err := (Claude{initializeTimeout: 150 * time.Millisecond}).Start(ctx, Launch{Directory: t.TempDir()}, func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() })
			if s != nil || err == nil || !strings.Contains(err.Error(), tc.want) || ctx.Err() != nil {
				t.Fatal("incorrect startup failure", err)
			}
			if tc.name == "early exit" && (len(frames) != 1 || frames[0].Raw != "last diagnostic") {
				t.Fatal("final partial line lost", frames)
			}
			if tc.name == "rejection" && (len(frames) != 1 || !strings.Contains(frames[0].Raw, "rejected")) {
				t.Fatal("error response not captured", frames)
			}
		})
	}
}

func TestInitializationResponseMatching(t *testing.T) {
	for _, raw := range []string{`{}`, `invalid`, `{"type":"system","response":{"request_id":"id","subtype":"success"}}`, `{"type":"control_response","response":{"request_id":"other","subtype":"success"}}`} {
		if matched, _ := initializationResponse([]byte(raw), "id"); matched {
			t.Fatal("unrelated response accepted")
		}
	}
	if matched, err := initializationResponse([]byte(`{"type":"control_response","response":{"request_id":"id","subtype":"unexpected"}}`), "id"); !matched || err == nil {
		t.Fatal("malformed matching response accepted")
	}
}

func TestClaudeAcknowledgementThenExitDrainsFinalOutput(t *testing.T) {
	fakeClaude(t, readInitialization+acknowledgeInitialization+"printf 'final partial line' >&2\n")
	var mu sync.Mutex
	var frames []codethreads.Payload
	s, err := (Claude{}).Start(t.Context(), Launch{Directory: t.TempDir()}, func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatal("final output lost", frames)
	}
}

func TestClaudeCancellationDuringInitialization(t *testing.T) {
	fakeClaude(t, readInitialization+"exec /bin/sleep 10\n")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	s, err := (Claude{}).Start(ctx, Launch{Directory: t.TempDir()}, func(codethreads.Payload) {})
	if s != nil || err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("cancellation did not clean up startup", err)
	}
}
