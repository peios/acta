package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/agentsession/model"
)

func lines(ss ...string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(ss))
	for _, s := range ss {
		out = append(out, json.RawMessage(s))
	}
	return out
}

func ords(recs []TranscriptRecord) string {
	var ids []string
	for _, r := range recs {
		var m struct {
			Ordinal int `json:"ordinal"`
		}
		_ = json.Unmarshal(r.Payload, &m)
		ids = append(ids, strings.TrimSpace(strings.Repeat(" ", 0)+itoa(m.Ordinal)))
	}
	return strings.Join(ids, " ")
}

func TestChainRecordsKeepsConversationOnly(t *testing.T) {
	recs := ChainRecords(lines(
		`{"ordinal":0,"type":"session_meta","payload":{"id":"th","cwd":"/w"}}`,
		`{"ordinal":1,"type":"event_msg","timestamp":"2026-09-05T23:44:19.524Z","payload":{"type":"task_started","turn_id":"T1"}}`,
		`{"ordinal":2,"type":"response_item","payload":{"type":"message","role":"user"}}`,
		`{"ordinal":3,"type":"turn_context","payload":{"turn_id":"T1","model":"gpt-5.6-luna","effort":"high"}}`,
		`{"ordinal":4,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"hi"}]}}}`,
		`{"ordinal":5,"type":"event_msg","payload":{"type":"user_message","message":"hi"}}`,
		`{"ordinal":6,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m1","content":[{"type":"Text","text":"hello"}]}}}`,
		`{"ordinal":7,"type":"token_usage_record","payload":{}}`,
		`{"ordinal":8,"type":"event_msg","payload":{"type":"token_count","info":{}}}`,
		`{"ordinal":9,"type":"event_msg","payload":{"type":"task_complete","turn_id":"T1"}}`,
		`{"ordinal":10,"type":"compacted","payload":{"replacement_history":[]}}`,
		`{"ordinal":11,"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-5.6-luna"}}}`,
	))
	// the turn's start is not conversation on its own, so the batch begins
	// at the first item; the user_message twin of the item is dropped; the
	// trailing settings are trimmed but the turn's completion stays
	if got, want := ords(recs), "4 6 8 9"; got != want {
		t.Errorf("records:\n got %s\nwant %s", got, want)
	}
	// with an item after them, the turn bookkeeping between items stays
	recs = ChainRecords(lines(
		`{"ordinal":4,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[]}}}`,
		`{"ordinal":8,"type":"event_msg","payload":{"type":"token_count","info":{}}}`,
		`{"ordinal":9,"type":"event_msg","payload":{"type":"task_complete","turn_id":"T1"}}`,
		`{"ordinal":12,"type":"event_msg","payload":{"type":"task_started","turn_id":"T2"}}`,
		`{"ordinal":13,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u2","content":[]}}}`,
	))
	if got, want := ords(recs), "4 8 9 12 13"; got != want {
		t.Errorf("records:\n got %s\nwant %s", got, want)
	}
	// an old rollout with no items keeps its message events
	recs = ChainRecords(lines(
		`{"ordinal":1,"type":"event_msg","payload":{"type":"user_message","message":"hi"}}`,
		`{"ordinal":2,"type":"event_msg","payload":{"type":"agent_message","message":"hello"}}`,
	))
	if got, want := ords(recs), "1 2"; got != want {
		t.Errorf("old records:\n got %s\nwant %s", got, want)
	}
}

func TestLeaf(t *testing.T) {
	if got := Leaf("item/completed", json.RawMessage(`{"method":"item/completed","params":{"item":{"id":"m1","type":"agentMessage"}}}`)); got != "m1" {
		t.Errorf("live leaf = %q", got)
	}
	if got := Leaf(TranscriptKind, json.RawMessage(`{"type":"event_msg","payload":{"type":"item_completed","item":{"id":"m2"}}}`)); got != "m2" {
		t.Errorf("transcript leaf = %q", got)
	}
	if got := Leaf(TranscriptKind, json.RawMessage(`{"type":"event_msg","payload":{"type":"task_complete"}}`)); got != "" {
		t.Errorf("task_complete is not a leaf: %q", got)
	}
	if got := TranscriptGlob("s", map[string]any{"conversation": "01a0"}); got != "~/.codex/sessions/*/*/*/rollout-*-01a0.jsonl" {
		t.Errorf("glob = %q", got)
	}
}

func TestScanTranscripts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "09", "06")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, ls ...string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("rollout-2026-09-06T00-43-47-01a073f4-f1e0-7cc3-9418-603b3f9e6dcf.jsonl",
		`{"timestamp":"2026-09-05T23:44:19.524Z","ordinal":0,"type":"session_meta","payload":{"id":"01a073f4-f1e0-7cc3-9418-603b3f9e6dcf","cwd":"/home/x/proj","originator":"acta","cli_version":"0.153.4","source":"vscode"}}`,
		`{"timestamp":"2026-09-05T23:44:19.818Z","ordinal":2,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<recommended_plugins>…"}]}}`,
		`{"timestamp":"2026-09-05T23:44:19.819Z","ordinal":5,"type":"turn_context","payload":{"turn_id":"T1","cwd":"/home/x/proj","model":"gpt-5.6-luna","effort":"high"}}`,
		`{"timestamp":"2026-09-05T23:44:19.835Z","ordinal":7,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"Hi Codex. Can you see this message?"}]}}}`,
		`{"timestamp":"2026-09-05T23:44:21.006Z","ordinal":8,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m1","content":[{"type":"Text","text":"Yes."}]}}}`,
	)
	write("rollout-2026-09-06T01-00-00-01a073f4-0000-7cc3-9418-603b3f9e6dcf.jsonl",
		`{"timestamp":"2026-09-06T00:00:00Z","ordinal":0,"type":"session_meta","payload":{"id":"01a073f4-0000-7cc3-9418-603b3f9e6dcf","cwd":"/home/x/proj","source":{"subagent":{"other":"guardian"}},"thread_source":"guardian_review"}}`,
		`{"timestamp":"2026-09-06T00:00:01Z","ordinal":1,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"review this"}]}}}`,
	)
	if err := os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"), []byte(`{"id":"01a073f4-f1e0-7cc3-9418-603b3f9e6dcf","thread_name":"First name"}`+"\n"+`{"id":"01a073f4-f1e0-7cc3-9418-603b3f9e6dcf","thread_name":"Codex Test"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ScanTranscripts(home)
	if len(got) != 1 {
		t.Fatalf("want 1 transcript (the reviewer's own thread skipped), got %d: %+v", len(got), got)
	}
	tr := got[0]
	if tr.ID != "01a073f4-f1e0-7cc3-9418-603b3f9e6dcf" || tr.Cwd != "/home/x/proj" || tr.Title != "Codex Test" || tr.First != "Hi Codex. Can you see this message?" || tr.Model != "gpt-5.6-luna" || tr.Version != "0.153.4" {
		t.Errorf("scan: %+v", tr)
	}
	if !tr.Started.Equal(time.Date(2026, 9, 5, 23, 44, 19, 524000000, time.UTC)) || !tr.Updated.Equal(time.Date(2026, 9, 5, 23, 44, 21, 6000000, time.UTC)) {
		t.Errorf("times: %v %v", tr.Started, tr.Updated)
	}
}

func TestTranscriptProjection(t *testing.T) {
	fs := frames(
		"state", `{"state":"import","count":8,"from":"2026-09-05T23:44:19Z","to":"2026-09-05T23:51:19Z"}`,
		"transcript", `{"type":"turn_context","payload":{"turn_id":"T1","model":"gpt-5.6-luna","effort":"high","approval_policy":"on-request","approvals_reviewer":"auto_review","sandbox_policy":{"type":"workspace-write"}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"task_started","turn_id":"T1","started_at":1788651859,"model_context_window":258400}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T1","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"list the files"}]}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T1","item":{"type":"Reasoning","id":"r1","summary_text":["**Listing**"],"raw_content":[]}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T1","item":{"type":"CommandExecution","id":"c1","command":["/usr/bin/bash","-lc","ls"],"cwd":"file:///w","parsed_cmd":[{"type":"list_files","cmd":"ls"}],"status":"completed","aggregated_output":"README.md\n","exit_code":0,"duration":{"secs":0,"nanos":2320000}}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T1","item":{"type":"FileChange","id":"f1","status":"completed","changes":{"/w/notes.txt":{"type":"add","content":"hello\n"}}}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T1","item":{"type":"McpToolCall","id":"x1","server":"acta","tool":"list_items","arguments":{"workspace":"peios"},"status":"completed","result":{"content":[{"type":"text","text":"{\"items\":[]}"}]},"error":null,"duration":{"secs":0,"nanos":168488098}}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T1","item":{"type":"AgentMessage","id":"m1","content":[{"type":"Text","text":"One file: README.md"}],"phase":"final_answer"}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":22248,"output_tokens":12,"total_tokens":22260},"last_token_usage":{"input_tokens":22248,"output_tokens":12,"total_tokens":22260},"model_context_window":258400}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"T1","last_agent_message":"One file: README.md","duration_ms":1500}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T2","item":{"type":"ContextCompaction","id":"cc1"}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"task_started","turn_id":"T3","started_at":1788651900}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"item_completed","thread_id":"th","turn_id":"T3","item":{"type":"UserMessage","id":"u2","content":[{"type":"text","text":"stop"}]}}}`,
		"transcript", `{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"T3","reason":"interrupted","duration_ms":90018}}`,
		"state", `{"state":"spawned","resumed":true}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	got := kinds(evs)
	for _, want := range []string{"session.catchup", "user.message", "thought", "tool.call tool.result→tool:c1", "tool.call tool.result→tool:f1", "tool.call tool.result→tool:x1", "assistant", "usage.context", "turn.end turn.idle", "compact.start", "compact.end→compact:12"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, e := range evs {
		switch e.T {
		case model.SessionCatchup:
			if e.Data["source"] != "import" || e.Data["count"] != 8 {
				t.Errorf("catchup: %v", e.Data)
			}
		case model.ToolCall:
			if e.Data["id"] == "c1" {
				if in, _ := e.Data["input"].(map[string]any); in == nil || in["command"] != "ls" || in["cwd"] != "/w" {
					t.Errorf("command call input: %v", e.Data["input"])
				}
			}
		case model.ToolResult:
			switch e.Data["call_id"] {
			case "c1":
				if e.Data["text"] != "README.md\n" || e.Data["exit_code"] != 0 || e.Data["duration_ms"] != int64(2) {
					t.Errorf("command result: %v", e.Data)
				}
			case "f1":
				d, _ := e.Data["diff"].(map[string]any)
				if d == nil || d["file"] != "/w/notes.txt" || d["kind"] != "create" || d["content"] != "hello\n" {
					t.Errorf("file change result: %v", e.Data)
				}
			case "x1":
				if e.Data["name"] != "mcp__acta__list_items" || e.Data["role"] != "acta" || e.Data["error"] != false {
					t.Errorf("mcp result: %v", e.Data)
				}
			}
		case model.Thought:
			if e.Data["text"] != "**Listing**" {
				t.Errorf("thought: %v", e.Data)
			}
		case model.TurnEnd:
			if e.Seq == 11 && (e.Data["ok"] != true || e.Data["result"] != "One file: README.md" || e.Data["duration_ms"] != int64(1500) || e.Data["context_window"] != int64(258400)) {
				t.Errorf("turn.end: %v", e.Data)
			}
			if e.Seq == 15 && (e.Data["interrupted"] != true || e.Data["ok"] != false) {
				t.Errorf("aborted turn.end: %v", e.Data)
			}
		case model.Assistant:
			if e.Data["model"] != "gpt-5.6-luna" {
				t.Errorf("assistant model from turn context: %v", e.Data["model"])
			}
		}
	}
	for _, e := range evs {
		for _, r := range e.Raw {
			if r.Kind != "transcript" && r.Kind != "state" {
				t.Errorf("event %s shows a synthesised %s frame as evidence", e.T, r.Kind)
			}
		}
	}
}
