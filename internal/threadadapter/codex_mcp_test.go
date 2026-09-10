package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestCodexMCPToolResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		result  any
		failure any
		want    string
		status  string
		unknown bool
	}{
		{"text", object{"content": []any{object{"type": "text", "text": "hello"}}}, nil, "hello", "completed", false},
		{"duplicate structured", object{"content": []any{object{"type": "text", "text": "{\"ok\":true}"}}, "structuredContent": object{"ok": true}}, nil, "{\"ok\":true}", "completed", false},
		{"structured only", object{"content": []any{}, "structuredContent": object{"ok": true}}, nil, "{\n  \"ok\": true\n}", "completed", false},
		{"error", nil, object{"message": "Denied"}, "Denied", "failed", false},
		{"rich", object{"content": []any{object{"type": "image", "data": "x", "mimeType": "image/png"}}}, nil, "", "completed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
			s := State{RunID: id, NativeID: "native"}
			call := func(seq int64, method, status string, result, failure any) []threads.Frame {
				m := object{"method": method, "params": object{"threadId": "native", "turnId": "turn", "item": object{"type": "mcpToolCall", "id": "tool", "server": "example", "tool": "lookup", "status": status, "arguments": object{}, "result": result, "error": failure, "durationMs": 35}}}
				raw, _ := json.Marshal(m)
				next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: time.Now()}, "stdout", string(raw))
				if err != nil {
					t.Fatal(err)
				}
				s = next
				return frames
			}
			start := call(1, "item/started", "inProgress", nil, nil)
			if len(start) != 2 || start[1].Kind != "tool/call" {
				t.Fatal(start)
			}
			done := call(2, "item/completed", tc.status, tc.result, tc.failure)
			if tc.unknown {
				if len(done) != 1 || done[0].Kind != "debug/unknown" || s.Tools["tool"].Status != "running" {
					t.Fatal(done)
				}
				return
			}
			if len(done) != 2 || done[1].Kind != "tool/call" || s.Tools["tool"].Status != tc.status || s.Tools["tool"].Output == nil || *s.Tools["tool"].Output != tc.want {
				t.Fatalf("%+v %+v", done, s.Tools["tool"])
			}
			repeat := call(3, "item/completed", tc.status, tc.result, tc.failure)
			if len(repeat) != 1 {
				t.Fatal("duplicate result emitted another snapshot")
			}
		})
	}
}
