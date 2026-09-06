package claude

import "testing"

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
