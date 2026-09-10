package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCapturedToolCalls(t *testing.T) {
	raw, err := os.ReadFile("testdata/tool-calls.json")
	if err != nil {
		t.Fatal(err)
	}
	var inputs []struct {
		Provider string
		Raw      object
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			s := State{RunID: id, Claude: ClaudeState{Turn: "turn"}}
			completed, failed, outputs, args := 0, 0, 0, 0
			for i, input := range inputs {
				if input.Provider != provider {
					continue
				}
				if provider == "codex" {
					s.NativeID = str(obj(input.Raw["params"])["threadId"])
				} else {
					s.NativeID = str(input.Raw["session_id"])
				}
				text, _ := json.Marshal(input.Raw)
				next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: int64(i + 1), ReceivedAt: time.Now()}, "stdout", string(text))
				if err != nil {
					t.Fatal(err)
				}
				s = next
				for _, f := range frames {
					if f.Kind == "debug/unknown" {
						t.Fatalf("unmapped %s: %s", text, f.Data)
					}
					var d object
					json.Unmarshal(f.Data, &d)
					if f.Kind == "debug/local" && d["processing_error"] != nil {
						t.Fatalf("invalid mapping: %s", f.Data)
					}
					if f.Kind == "tool/call" && terminalTool(str(d["status"])) {
						completed++
						if d["status"] == "failed" {
							failed++
						}
					}
					if f.Kind == "tool/output/delta" {
						outputs++
					}
					if f.Kind == "tool/arguments/delta" {
						args++
					}
					if provider == "claude" && d["status"] == "running" {
						t.Fatal("invented execution start")
					}
				}
				// Replay restoration between every capture exercises persisted block links.
				saved, _ := json.Marshal(s)
				s, err = DecodeState(saved)
				if err != nil {
					t.Fatal(err)
				}
			}
			if provider == "codex" && (completed != 2 || failed != 0 || outputs != 1) {
				t.Fatalf("codex results %d %d %d", completed, failed, outputs)
			}
			if provider == "claude" && (completed != 3 || failed != 1 || args != 15) {
				t.Fatalf("claude results %d %d %d", completed, failed, args)
			}
		})
	}
}

func TestToolUnknownIdentityAndUnsupportedContent(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, raw := range []string{
		`{"method":"item/started","params":{"threadId":"wrong","turnId":"t","item":{"type":"commandExecution","id":"x","command":"ls","status":"inProgress"}}}`,
		`{"method":"item/commandExecution/outputDelta","params":{"threadId":"native","turnId":"t","itemId":"missing","delta":"hello"}}`,
		`{"type":"user","session_id":"native","message":{"content":[{"type":"tool_result","tool_use_id":"missing","content":"hi"}]}}`,
		`{"type":"assistant","session_id":"native","message":{"content":[{"type":"tool_use","id":"x","name":"","input":{}}]}}`,
	} {
		provider := "codex"
		var m object
		json.Unmarshal([]byte(raw), &m)
		if m["type"] != nil {
			provider = "claude"
		}
		_, f, err := Map(State{RunID: id, NativeID: "native", Claude: ClaudeState{Turn: "t"}}, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: 1, ReceivedAt: time.Now()}, "stdout", raw)
		if err != nil || len(f) != 1 || f[0].Kind != "debug/unknown" {
			t.Fatalf("unexpected mapping: %v %+v", err, f)
		}
	}
}

// The denial/result fields match the captured Write rejection. Reordering them
// exercises the two independent provider notifications, with checkpoint restore
// between every input as happens when the hyperharness reconnects.
func TestGenericToolsAndPermissionDenials(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, name := range []string{"Write", "Edit", "UnfamiliarTool", "mcp__example__lookup"} {
		for _, order := range []string{"denial-first", "result-first", "result-only", "ordinary-error", "success"} {
			t.Run(name+"/"+order, func(t *testing.T) {
				s := State{RunID: id, NativeID: "native", Claude: ClaudeState{Turn: "turn", Message: "msg", Blocks: map[string]string{}}}
				sequence := int64(0)
				apply := func(m object) {
					t.Helper()
					m["session_id"] = "native"
					raw, _ := json.Marshal(m)
					sequence++
					next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: sequence, ReceivedAt: time.Now()}, "stdout", string(raw))
					if err != nil {
						t.Fatal(err)
					}
					for _, frame := range frames {
						if frame.Kind == "debug/unknown" {
							t.Fatalf("unmapped: %s", raw)
						}
						var d object
						json.Unmarshal(frame.Data, &d)
						if d["processing_error"] != nil {
							t.Fatalf("invalid frame: %s", frame.Data)
						}
					}
					checkpoint, _ := json.Marshal(next)
					s, err = DecodeState(checkpoint)
					if err != nil {
						t.Fatal(err)
					}
				}
				apply(object{"type": "stream_event", "event": object{"type": "content_block_start", "index": 0, "content_block": object{"type": "tool_use", "id": "call", "name": name, "input": object{}}}})
				apply(object{"type": "stream_event", "event": object{"type": "content_block_delta", "index": 0, "delta": object{"type": "input_json_delta", "partial_json": `{"file_path":"/tmp/check.txt","content":"test"}`}}})
				apply(object{"type": "stream_event", "event": object{"type": "content_block_stop", "index": 0}})
				if s.Tools["call"].Status != "pending" {
					t.Fatal("arguments completed must be pending")
				}
				denial := object{"type": "system", "subtype": "permission_denied", "tool_name": name, "tool_use_id": "call", "decision_reason_type": "workingDir", "decision_reason": "Path is outside allowed working directories", "message": "Write permission was not granted."}
				result := object{"type": "user", "message": object{"content": []any{object{"type": "tool_result", "tool_use_id": "call", "is_error": order != "success", "content": "Exact provider result\n"}}}}
				if order == "result-only" {
					result["tool_result_meta"] = []any{object{"id": "call", "non_execution_kind": "user-rejected"}}
				}
				if order == "denial-first" {
					apply(denial)
				}
				apply(result)
				if order == "result-first" {
					apply(denial)
				}
				// A repeated result and delayed argument snapshot cannot undo denial.
				apply(result)
				apply(object{"type": "assistant", "message": object{"content": []any{object{"type": "tool_use", "id": "call", "name": name, "input": object{}}}}})
				call := s.Tools["call"]
				want := "permission_denied"
				if order == "ordinary-error" {
					want = "failed"
				}
				if order == "success" {
					want = "completed"
				}
				if call.Status != want || call.Output == nil || *call.Output != "Exact provider result\n" {
					t.Fatalf("bad result: %+v", call)
				}
				if call.Started != nil || call.Duration != nil {
					t.Fatal("invented execution timing")
				}
				if order == "denial-first" || order == "result-first" {
					if call.PermissionDenial["reason_type"] != "workingDir" {
						t.Fatal("lost denial reason")
					}
				}
				if name == "Write" || name == "Edit" {
					if call.Label != name+" check.txt" {
						t.Fatal(call.Label)
					}
				} else if call.Category != "generic" || call.Label != name {
					t.Fatalf("missing generic presentation: %+v", call)
				}
			})
		}
	}
}

func TestClaudeToolSearchReferences(t *testing.T) {
	for _, scenario := range []string{"valid", "empty", "wrong-tool", "unknown-part"} {
		t.Run(scenario, func(t *testing.T) {
			name := "ToolSearch"
			if scenario == "wrong-tool" {
				name = "OtherTool"
			}
			parts := []any{object{"type": "text", "text": "Found:"}, object{"type": "tool_reference", "tool_name": "TaskOutput"}, object{"type": "tool_reference", "tool_name": "Read"}}
			if scenario == "empty" {
				parts = append(parts, object{"type": "tool_reference", "tool_name": ""})
			}
			if scenario == "unknown-part" {
				parts = append(parts, object{"type": "future-content"})
			}
			s := State{RunID: "run", Claude: ClaudeState{Turn: "turn"}, Tools: map[string]*ToolCall{"tool": {ID: "tool", Name: name, Turn: "turn", Status: "pending"}}}
			var output string
			var calls int
			handled := s.claudeTool(threads.ProviderFrame{RunID: "run", ReceivedAt: time.Now()}, object{"type": "user", "message": object{"content": []any{object{"type": "tool_result", "tool_use_id": "tool", "content": parts}}}}, func(kind string, d object) {
				if kind == "tool/call" {
					calls++
					if text, ok := d["output"].(*string); ok && text != nil {
						output = *text
					}
				}
			})
			if scenario != "valid" {
				if handled || calls != 0 || s.Tools["tool"].ResultReceived {
					t.Fatal("unreviewed result was partially consumed")
				}
				return
			}
			if !handled || calls != 1 || output != "Found:\nAvailable tool: TaskOutput\nAvailable tool: Read" || s.Tools["tool"].Status != "completed" {
				t.Fatalf("handled=%v calls=%d output=%q tool=%+v", handled, calls, output, s.Tools["tool"])
			}
		})
	}
}
