package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClaudeObservedTextTurn(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-text-turn.json")
	if err != nil {
		t.Fatal(err)
	}
	var inputs []json.RawMessage
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	state := State{NativeID: id}
	counts := map[string]int{}
	completedText := ""
	for i, input := range inputs {
		p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(i + 1), ReceivedAt: time.Now().UTC()}
		var frames []threads.Frame
		state, frames, err = Map(state, p, "stdout", string(input))
		if err != nil {
			t.Fatal(err)
		}
		if frames[0].Kind == "debug/unknown" {
			t.Errorf("unmapped observed frame %d: %s", i, input)
		}
		for _, frame := range frames {
			var data object
			_ = json.Unmarshal(frame.Data, &data)
			if data["processing_error"] != nil {
				t.Errorf("frame %d: %s", i, frame.Data)
			}
			counts[frame.Kind]++
			if frame.Kind == "usage/context" && obj(data["context"])["capacity_tokens"] == nil {
				usage := obj(data["last_request"])
				if usage["input_tokens"] != float64(5517) || usage["output_tokens"] != float64(6) {
					t.Errorf("request usage lost across delta state: %v", usage)
				}
			}
			if frame.Kind == "message/assistant" && data["state"] == "completed" {
				completedText = str(data["text"])
				if data["phase"] != nil {
					t.Error("invented phase")
				}
			}
		}
	}
	if completedText != "Message received." {
		t.Errorf("final text %q", completedText)
	}
	for _, kind := range []string{"message/user", "message/assistant/delta", "turn/completed", "usage/context", "usage/account", "thread/configuration", "thread/status"} {
		if counts[kind] == 0 {
			t.Errorf("missing %s", kind)
		}
	}
	if counts["message/user"] != 1 || counts["turn/completed"] != 1 {
		t.Errorf("duplicate lifecycle: %v", counts)
	}
}
func TestClaudeUnknownAndSessionIsolation(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}
	for _, input := range []string{`{"type":"system","subtype":"future","session_id":"` + id + `"}`, `{"type":"command_lifecycle","command_uuid":"x","state":"started","session_id":"wrong"}`, `{"type":"control_request","request_id":"approval","request":{"subtype":"can_use_tool"}}`} {
		_, frames, err := Map(State{NativeID: id}, p, "stdout", input)
		if err != nil || len(frames) != 1 || frames[0].Kind != "debug/unknown" {
			t.Fatalf("unsupported frame lost: %s %v", input, frames)
		}
	}
}

func TestClaudeLocalCommandIsNotAUserTurn(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}
	input := `{"type":"user","uuid":"receipt","session_id":"` + id + `","isReplay":true,"message":{"content":"<local-command-stdout>Set model</local-command-stdout>"}}`
	state, frames, err := Map(State{NativeID: id}, p, "stdout", input)
	if err != nil || len(frames) != 1 || frames[0].Kind != "debug/local" || state.Claude.Turn != "" {
		t.Fatal("command receipt became chat", frames, err)
	}
}

func TestClaudeMultipleTextBlocksKeepStreamOrder(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	state := State{NativeID: id}
	p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", ReceivedAt: time.Now()}
	final := ""
	inputs := []string{
		`{"type":"command_lifecycle","command_uuid":"turn","state":"started"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"First. "}}}`,
		`{"type":"assistant","message":{"id":"msg","content":[{"type":"text","text":"First. "}]}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Second."}}}`,
		`{"type":"assistant","message":{"id":"msg","content":[{"type":"text","text":"Second."}]}}`,
		`{"type":"stream_event","event":{"type":"message_stop"}}`,
	}
	for _, input := range inputs {
		var m object
		_ = json.Unmarshal([]byte(input), &m)
		m["session_id"] = id
		raw, _ := json.Marshal(m)
		p.Sequence++
		next, frames, err := Map(state, p, "stdout", string(raw))
		if err != nil {
			t.Fatal(err)
		}
		state = next
		for _, f := range frames {
			var d object
			_ = json.Unmarshal(f.Data, &d)
			if f.Kind == "message/assistant" && d["state"] == "completed" {
				final = str(d["text"])
			}
		}
	}
	if final != "First. Second." {
		t.Fatalf("block snapshot lost or duplicated text: %q", final)
	}
	if state.Claude.Messages["msg"].Text != "" {
		t.Fatal("completed body retained in reducer state")
	}
}

func TestClaudeConfigurationSecretsStayLocal(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}
	for call, body := range map[string]string{
		"get_settings": `{"applied":{"model":"safe","effort":null},"effective":{"env":{"ANTHROPIC_API_KEY":"secret"}},"sources":{"token":"secret"}}`,
		"initialize":   `{"session_state":"idle","account":{"token":"secret"}}`,
		"mcp_status":   `{"mcpServers":[{"name":"safe","status":"connected","config":{"headers":{"Authorization":"secret"}}}]}`,
	} {
		input := `{"type":"control_response","response":{"request_id":"` + id + `/` + call + `","subtype":"success","response":` + body + `}}`
		_, frames, err := Map(State{NativeID: id}, p, "stdout", input)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			if strings.Contains(string(frame.Data), "secret") {
				t.Fatalf("%s leaked local configuration", call)
			}
		}
		var debug object
		_ = json.Unmarshal(frames[0].Data, &debug)
		if debug["redacted"] != true {
			t.Fatal("redaction is not disclosed")
		}
	}
}

func TestClaudeCancelledLifecycleCorrectsSuccessfulResult(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "after_result", true: "before_result"}[before], func(t *testing.T) {
			state := State{NativeID: "native"}
			inputs := []string{`{"type":"command_lifecycle","command_uuid":"turn","state":"started"}`}
			result := `{"type":"result","user_message_uuid":"turn","subtype":"success","terminal_reason":"completed","duration_ms":9087}`
			cancel := `{"type":"command_lifecycle","command_uuid":"turn","state":"cancelled"}`
			if before {
				inputs = append(inputs, cancel, result)
			} else {
				inputs = append(inputs, result, cancel)
			}
			inputs = append(inputs, cancel, result)
			outcome := ""
			completions := 0
			for i, input := range inputs {
				p := threads.ProviderFrame{ThreadID: "3345d4d5-3427-4c76-8786-20cc947341c4", RunID: "3345d4d5-3427-4c76-8786-20cc947341c4", Provider: "claude", Sequence: int64(i + 1), ReceivedAt: time.Now()}
				var source object
				json.Unmarshal([]byte(input), &source)
				source["session_id"] = "native"
				raw, _ := json.Marshal(source)
				next, frames, err := Map(state, p, "stdout", string(raw))
				if err != nil {
					t.Fatal(err)
				}
				state = next
				var debug object
				json.Unmarshal(frames[0].Data, &debug)
				if frames[0].Kind == "debug/unknown" || debug["processing_error"] != nil {
					t.Fatal(input, string(frames[0].Data))
				}
				for _, f := range frames {
					if f.Kind == "turn/completed" {
						var d object
						json.Unmarshal(f.Data, &d)
						outcome = str(d["outcome"])
						completions++
					}
				}
			}
			if outcome != "interrupted" || completions != 2 {
				t.Fatalf("%s %d", outcome, completions)
			}
		})
	}
}

func TestClaudeInterruptionMarkerIsDropped(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}
	for _, marker := range []string{"[Request interrupted by user]", "[Request interrupted by user for tool use]"} {
		for _, replay := range []bool{false, true} {
			state := State{NativeID: id}
			if replay {
				state.Submissions = map[string]string{"message": id}
			}
			raw, _ := json.Marshal(object{"type": "user", "uuid": "message", "session_id": id, "isReplay": replay, "message": object{"role": "user", "content": []any{object{"type": "text", "text": marker}}}})
			_, frames, err := Map(state, p, "stdout", string(raw))
			if err != nil {
				t.Fatal(err)
			}
			if !replay && (len(frames) != 1 || frames[0].Kind != "debug/dropped") {
				t.Fatalf("marker was not dropped: %+v", frames)
			}
			if replay && (len(frames) != 2 || frames[1].Kind != "message/user") {
				t.Fatalf("real submitted message was hidden: %+v", frames)
			}
		}
	}
}

func TestQueuedClaudeInputDoesNotRetargetRunningTurn(t *testing.T) {
	s := State{NativeID: "native"}
	p := capture(1)
	p.Provider = "claude"
	next, _, err := Map(s, p, "stdout", `{"type":"command_lifecycle","session_id":"native","command_uuid":"first","state":"started"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.Sequence++
	next, frames, err := Map(next, p, "stdout", `{"type":"command_lifecycle","session_id":"native","command_uuid":"second","state":"queued"}`)
	if err != nil {
		t.Fatal(err)
	}
	if next.Claude.Turn != "first" {
		t.Fatal("queued input displaced active turn", next.Claude.Turn)
	}
	for _, f := range frames {
		if f.Kind == "turn/started" {
			t.Fatal("queued input started a premature turn")
		}
	}
	p.Sequence++
	next, _, err = Map(next, p, "stdout", `{"type":"command_lifecycle","session_id":"native","command_uuid":"second","state":"started"}`)
	if err != nil || next.Claude.Turn != "second" {
		t.Fatal("started input did not advance turn", err)
	}
}
