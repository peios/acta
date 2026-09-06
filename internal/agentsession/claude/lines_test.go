package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeepLineAndTurnStart checks the cheap line rules a harness applies
// before holding a transcript: only conversation records are kept, and a
// turn starts at a typed prompt, not at a tool result or a meta record.
func TestKeepLineAndTurnStart(t *testing.T) {
	cases := []struct {
		line       string
		keep, turn bool
	}{
		{`{"type":"user","message":{"role":"user","content":"hello"}}`, true, true},
		{`{"type":"user","message":{"content":[{"type":"text","text":"hi"}]}}`, true, true},
		{`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"out"}]}}`, true, false},
		{`{"type":"user","isMeta":true,"message":{"content":"<local-command-stdout>x</local-command-stdout>"}}`, true, false},
		{`{"type":"user","isSidechain":true,"message":{"content":"sub prompt"}}`, true, false},
		{`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`, true, false},
		{`{"type":"system","subtype":"turn_duration"}`, true, false},
		{`{"type":"file-history-snapshot","snapshot":{}}`, false, false},
		{`{"type":"attachment","uuid":"a1","parentUuid":"u1","attachment":{}}`, true, false},
		{`{"type":"queue-operation"}`, false, false},
		{`not json`, false, false},
	}
	for _, c := range cases {
		if got := KeepLine([]byte(c.line)); got != c.keep {
			t.Errorf("KeepLine(%s) = %v", c.line, got)
		}
		if got := TurnStart([]byte(c.line)); got != c.turn {
			t.Errorf("TurnStart(%s) = %v", c.line, got)
		}
	}
}

// TestWriteTitle checks the sidecar lands where ScanTranscripts reads it.
func TestWriteTitle(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-home-x")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "11111111-2222-4333-8444-555555555555.jsonl")
	line := `{"type":"user","uuid":"u1","cwd":"/home/x","timestamp":"2026-09-06T10:00:00Z","message":{"role":"user","content":"hello there"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteTitle(path, "[TODO] Fix it"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "11111111-2222-4333-8444-555555555555", "custom-title.json"))
	if err != nil || string(b) != `{"customTitle":"[TODO] Fix it"}` {
		t.Fatalf("sidecar: %s %v", b, err)
	}
	ts := ScanTranscripts(home)
	if len(ts) != 1 || ts[0].Title != "[TODO] Fix it" {
		t.Errorf("scan after write: %+v", ts)
	}
}
