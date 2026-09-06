package codex

import "testing"

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
