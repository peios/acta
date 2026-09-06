package codex

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeepLineAndTurnStart checks the cheap line rules a harness applies
// before holding a rollout: event and turn-context records are kept, the
// bulk of a rollout is not, and a turn starts at task_started.
func TestKeepLineAndTurnStart(t *testing.T) {
	cases := []struct {
		line       string
		keep, turn bool
	}{
		{`{"type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`, true, true},
		{`{"type":"event_msg","payload":{"type":"item_completed","item":{"id":"i1"}}}`, true, false},
		{`{"type":"event_msg","payload":{"type":"token_count"}}`, true, false},
		{`{"type":"turn_context","payload":{"model":"m"}}`, true, false},
		{`{"type":"response_item","payload":{"type":"reasoning"}}`, false, false},
		{`{"type":"compacted","payload":{}}`, false, false},
		{`{"type":"token_usage_record","payload":{}}`, false, false},
		{`{"type":"session_meta","payload":{"id":"x"}}`, false, false},
		{`{`, false, false},
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

// TestWriteTitle checks the appended index line is what ScanTranscripts
// reads as the thread's name, and that a later line wins.
func TestWriteTitle(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "09", "06")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := "01a0aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"
	lines := `{"timestamp":"2026-09-06T10:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"/home/x"}}` + "\n" +
		`{"timestamp":"2026-09-06T10:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"hello there"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-09-06T10-00-00-"+id+".jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteTitle(home, id, "First"); err != nil {
		t.Fatal(err)
	}
	if err := WriteTitle(home, id, "[DONE] Second"); err != nil {
		t.Fatal(err)
	}
	ts := ScanTranscripts(home)
	if len(ts) != 1 || ts[0].Title != "[DONE] Second" {
		t.Errorf("scan after write: %+v", ts)
	}
}
