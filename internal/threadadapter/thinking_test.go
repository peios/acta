package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestThinkingProviders(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			state := State{NativeID: id}
			seq := int64(0)
			send := func(input object) []threads.Frame {
				t.Helper()
				seq++
				raw, _ := json.Marshal(input)
				next, frames, err := Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: seq, ReceivedAt: time.Unix(100+seq, 0)}, "stdout", string(raw))
				if err != nil {
					t.Fatal(err)
				}
				state = next
				for _, f := range frames {
					if f.Kind == "debug/unknown" || (f.Kind == "debug/local" && len(frames) == 1 && str(input["method"]) == "item/completed") {
						t.Fatalf("unmapped %s: %s", raw, f.Data)
					}
				}
				return frames
			}
			var done []threads.Frame
			if provider == "codex" {
				call := func(method string, p object) []threads.Frame {
					p["threadId"] = id
					p["turnId"] = "turn"
					return send(object{"method": method, "params": p})
				}
				call("item/started", object{"item": object{"type": "reasoning", "id": "r", "summary": []any{}, "content": []any{}}, "startedAtMs": 100000})
				call("item/reasoning/summaryPartAdded", object{"itemId": "r", "summaryIndex": 0})
				call("item/reasoning/summaryTextDelta", object{"itemId": "r", "summaryIndex": 0, "delta": "Checking"})
				call("item/reasoning/summaryTextDelta", object{"itemId": "r", "summaryIndex": 1, "delta": "Next"})
				call("item/reasoning/textDelta", object{"itemId": "r", "contentIndex": 0, "delta": "Separate content"})
				done = call("item/completed", object{"item": object{"type": "reasoning", "id": "r", "summary": []any{"Checked", "Next"}, "content": []any{"Separate content"}}, "completedAtMs": 105000})
				if state.Thinking["r"].Estimate != nil {
					t.Fatal("invented token estimate")
				}
			} else {
				call := func(typ string, p object) []threads.Frame { p["session_id"] = id; p["type"] = typ; return send(p) }
				event := func(p object) []threads.Frame { return call("stream_event", object{"event": p}) }
				call("command_lifecycle", object{"command_uuid": "turn", "state": "started"})
				event(object{"type": "message_start", "message": object{"id": "msg"}})
				event(object{"type": "content_block_start", "index": 0, "content_block": object{"type": "thinking", "thinking": ""}})
				call("system", object{"subtype": "thinking_tokens", "estimated_tokens": 50})
				event(object{"type": "content_block_delta", "index": 0, "delta": object{"type": "thinking_delta", "thinking": "Checking", "estimated_tokens": 50}})
				event(object{"type": "content_block_delta", "index": 0, "delta": object{"type": "thinking_delta", "thinking": "", "estimated_tokens": nil}})
				call("system", object{"subtype": "thinking_tokens", "estimated_tokens": 141, "estimated_tokens_delta": 91})
				event(object{"type": "content_block_delta", "index": 0, "delta": object{"type": "signature_delta", "signature": "opaque"}})
				done = call("assistant", object{"message": object{"id": "msg", "content": []any{object{"type": "thinking", "thinking": "Checked\n\nNext", "signature": "opaque"}}}})
				f := event(object{"type": "content_block_stop", "index": 0})
				if len(f) != 1 {
					t.Fatal("duplicate completion")
				}
				if numeric(state.Thinking["msg/thinking/0"].Estimate) != 141 {
					t.Fatal("double counted estimate")
				}
				// A second block invalidates per-item attribution of the cumulative counter.
				f = event(object{"type": "content_block_start", "index": 1, "content_block": object{"type": "thinking", "thinking": ""}})
				if state.Thinking["msg/thinking/0"].Estimate != nil || len(f) != 3 {
					t.Fatal("ambiguous estimate retained")
				}
				call("system", object{"subtype": "thinking_tokens", "estimated_tokens": 200})
				if state.Thinking["msg/thinking/1"].Estimate != nil {
					t.Fatal("attributed cumulative counter to second block")
				}
			}
			var data object
			json.Unmarshal(done[len(done)-1].Data, &data)
			if done[len(done)-1].Kind != "thinking/completed" || data["text"] != "Checked\n\nNext" {
				t.Fatalf("bad completion: %s", done[len(done)-1].Data)
			}
			// Adapter state must round trip for reconnect without losing text or lifecycle.
			saved, _ := json.Marshal(state)
			restored, err := DecodeState(saved)
			if err != nil || len(restored.Thinking) != len(state.Thinking) {
				t.Fatal("state persistence", err)
			}
		})
	}
}
