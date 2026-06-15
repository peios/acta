package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterBootHookMergesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	script := filepath.Join(dir, "hooks", "acta-boot.sh")

	// Pre-existing settings: an unrelated top-level key and an unrelated
	// SessionStart hook that must both survive.
	seed := `{
  "model": "opus",
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "echo other"}]}
    ]
  }
}`
	if err := os.WriteFile(settingsPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := registerBootHook(settingsPath, script); err != nil {
		t.Fatal(err)
	}

	got := readSettings(t, settingsPath)
	if got["model"] != "opus" {
		t.Fatalf("unrelated key not preserved: model = %v", got["model"])
	}
	ss := sessionStart(t, got)
	if n := countRefers(ss, "echo other"); n != 1 {
		t.Fatalf("unrelated SessionStart hook lost: count = %d", n)
	}
	if n := countRefers(ss, bootHookMarker); n != len(bootHookMatchers) {
		t.Fatalf("want %d acta entries, got %d", len(bootHookMatchers), n)
	}

	// One entry per matcher, each a command hook pointing at the script with a
	// timeout.
	seen := map[string]bool{}
	for _, e := range ss {
		if !hookEntryRefersTo(e, bootHookMarker) {
			continue
		}
		m := e.(map[string]any)
		seen[m["matcher"].(string)] = true
		h := m["hooks"].([]any)[0].(map[string]any)
		if h["command"] != script {
			t.Fatalf("command = %v, want %s", h["command"], script)
		}
		if h["timeout"] != float64(8) {
			t.Fatalf("timeout = %v, want 8", h["timeout"])
		}
	}
	for _, want := range bootHookMatchers {
		if !seen[want] {
			t.Fatalf("missing matcher %q", want)
		}
	}

	// Re-running must not duplicate the acta entries nor drop the unrelated one.
	if err := registerBootHook(settingsPath, script); err != nil {
		t.Fatal(err)
	}
	ss = sessionStart(t, readSettings(t, settingsPath))
	if n := countRefers(ss, bootHookMarker); n != len(bootHookMatchers) {
		t.Fatalf("idempotent re-run: want %d acta entries, got %d", len(bootHookMatchers), n)
	}
	if n := countRefers(ss, "echo other"); n != 1 {
		t.Fatalf("idempotent re-run: unrelated hook count = %d", n)
	}
}

func TestRegisterBootHookCreatesFreshFile(t *testing.T) {
	dir := t.TempDir()
	// A nested path that doesn't exist yet — registerBootHook must create it.
	settingsPath := filepath.Join(dir, "claude", "settings.json")
	script := "/home/x/.claude/hooks/acta-boot.sh"

	if err := registerBootHook(settingsPath, script); err != nil {
		t.Fatal(err)
	}
	ss := sessionStart(t, readSettings(t, settingsPath))
	if len(ss) != len(bootHookMatchers) {
		t.Fatalf("fresh file: want %d entries, got %d", len(bootHookMatchers), len(ss))
	}
}

func TestRegisterBootHookRejectsMalformedSettings(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registerBootHook(settingsPath, "/x/acta-boot.sh"); err == nil {
		t.Fatal("want error on malformed settings.json, got nil")
	}
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json not valid JSON: %v", err)
	}
	return m
}

func sessionStart(t *testing.T, settings map[string]any) []any {
	t.Helper()
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("no hooks object in settings")
	}
	ss, ok := hooks["SessionStart"].([]any)
	if !ok {
		t.Fatalf("no SessionStart array in hooks")
	}
	return ss
}

func countRefers(entries []any, needle string) int {
	n := 0
	for _, e := range entries {
		if hookEntryRefersTo(e, needle) {
			n++
		}
	}
	return n
}
