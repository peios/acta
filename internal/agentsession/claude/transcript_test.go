package claude

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

func uuids(recs []TranscriptRecord) string {
	var ids []string
	for _, r := range recs {
		var m struct {
			UUID string `json:"uuid"`
		}
		_ = json.Unmarshal(r.Payload, &m)
		ids = append(ids, m.UUID)
	}
	return strings.Join(ids, " ")
}

func TestChainRecordsKeepsTheLiveBranch(t *testing.T) {
	recs := ChainRecords(lines(
		`{"type":"queue-operation","operation":"enqueue"}`,
		`{"type":"attachment","uuid":"att","parentUuid":null,"attachment":{"type":"hook_success"}}`,
		`{"type":"user","uuid":"u1","parentUuid":"att","timestamp":"2026-09-05T13:00:00Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-09-05T13:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-09-05T13:00:02Z","message":{"role":"user","content":"abandoned"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-09-05T13:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"gone"}]}}`,
		`{"type":"user","uuid":"u3","parentUuid":"a1","timestamp":"2026-09-05T13:00:04Z","message":{"role":"user","content":"rewound"}}`,
		`{"type":"assistant","uuid":"a3","parentUuid":"u3","timestamp":"2026-09-05T13:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"system","uuid":"td","parentUuid":"a3","subtype":"turn_duration","durationMs":900,"timestamp":"2026-09-05T13:00:06Z"}`,
		`{"type":"system","uuid":"b1","parentUuid":null,"logicalParentUuid":"td","subtype":"compact_boundary","compactMetadata":{"preTokens":100,"postTokens":10,"trigger":"auto"},"timestamp":"2026-09-05T13:00:07Z"}`,
		`{"type":"user","uuid":"s1","parentUuid":"b1","isCompactSummary":true,"timestamp":"2026-09-05T13:00:08Z","message":{"role":"user","content":"summary"}}`,
		`{"type":"last-prompt","leafUuid":"s1"}`,
	))
	if got, want := uuids(recs), "u1 a1 u3 a3 td b1 s1"; got != want {
		t.Errorf("chain:\n got %s\nwant %s", got, want)
	}
	if recs[0].At.IsZero() || recs[0].At.Hour() != 13 {
		t.Errorf("timestamp not read: %v", recs[0].At)
	}
}

func TestChainRecordsTrimsWhatIsNotConversation(t *testing.T) {
	// a catch-up that finds only the tail of Acta's own last turn stores nothing
	recs := ChainRecords(lines(
		`{"type":"system","uuid":"td","parentUuid":"a9","subtype":"turn_duration","durationMs":900}`,
		`{"type":"ai-title","aiTitle":"x"}`,
	))
	if len(recs) != 0 {
		t.Errorf("want nothing, got %s", uuids(recs))
	}
	// a partial batch (after Acta's leaf, whose parent is not in the lines)
	// is the live branch from where the chain breaks
	recs = ChainRecords(lines(
		`{"type":"system","uuid":"td","parentUuid":"a9","subtype":"turn_duration"}`,
		`{"type":"user","uuid":"u5","parentUuid":"td","message":{"role":"user","content":"more"}}`,
		`{"type":"assistant","uuid":"a5","parentUuid":"u5","message":{"role":"assistant","content":[{"type":"text","text":"sure"}]}}`,
		`{"type":"system","uuid":"td2","parentUuid":"a5","subtype":"turn_duration"}`,
	))
	if got, want := uuids(recs), "u5 a5 td2"; got != want {
		t.Errorf("partial chain:\n got %s\nwant %s", got, want)
	}
}

func TestLeaf(t *testing.T) {
	if got := Leaf("assistant", json.RawMessage(`{"type":"assistant","uuid":"a1"}`)); got != "a1" {
		t.Errorf("assistant leaf = %q", got)
	}
	if got := Leaf("user", json.RawMessage(`{"type":"user","uuid":"u1","isReplay":true}`)); got != "u1" {
		t.Errorf("user leaf = %q", got)
	}
	if got := Leaf(TranscriptKind, json.RawMessage(`{"type":"system","uuid":"s1"}`)); got != "" {
		t.Errorf("system record is not a leaf: %q", got)
	}
	if got := Leaf("result", json.RawMessage(`{"type":"result","uuid":"r1"}`)); got != "" {
		t.Errorf("result is not a leaf: %q", got)
	}
}

func TestTranscriptGlob(t *testing.T) {
	if got := TranscriptGlob("abc", nil); got != "~/.claude/projects/*/abc.jsonl" {
		t.Errorf("glob = %q", got)
	}
	if got := TranscriptGlob("abc", map[string]any{"conversation": "def"}); got != "~/.claude/projects/*/def.jsonl" {
		t.Errorf("glob after /clear = %q", got)
	}
}

func TestScanTranscripts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-home-x-proj")
	if err := os.MkdirAll(filepath.Join(dir, "s1"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, ls ...string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("s1.jsonl",
		`{"type":"queue-operation","operation":"enqueue","content":"Fix the build"}`,
		`{"type":"user","uuid":"u1","parentUuid":null,"cwd":"/home/x/proj","version":"2.1.261","timestamp":"2026-09-01T10:00:00Z","message":{"role":"user","content":"<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"}}`,
		`{"type":"user","uuid":"u2","parentUuid":"u1","cwd":"/home/x/proj","timestamp":"2026-09-01T10:00:01Z","message":{"role":"user","content":"Fix the build\nplease"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u2","timestamp":"2026-09-01T10:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"On it"}]}}`,
		`{"type":"ai-title","aiTitle":"Build fix"}`,
		`{"type":"permission-mode","permissionMode":"acceptEdits"}`,
	)
	if err := os.WriteFile(filepath.Join(dir, "s1", "custom-title.json"), []byte(`{"customTitle":"My build"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	write("s2.jsonl",
		`{"type":"user","uuid":"u1","parentUuid":null,"cwd":"/home/x/proj","timestamp":"2026-09-02T10:00:00Z","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"}}`,
	)
	write("agent-abc.jsonl", `{"type":"user","uuid":"u1","parentUuid":null,"isSidechain":true,"timestamp":"2026-09-03T10:00:00Z","message":{"role":"user","content":"sub"}}`)
	got := ScanTranscripts(home)
	if len(got) != 1 {
		t.Fatalf("want 1 transcript (no prompt in s2, agent file skipped), got %d: %+v", len(got), got)
	}
	tr := got[0]
	if tr.ID != "s1" || tr.Cwd != "/home/x/proj" || tr.Title != "My build" || tr.First != "Fix the build" || tr.Mode != "acceptEdits" || tr.Version != "2.1.261" {
		t.Errorf("scan: %+v", tr)
	}
	if !tr.Started.Equal(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)) || !tr.Updated.Equal(time.Date(2026, 9, 1, 10, 0, 5, 0, time.UTC)) {
		t.Errorf("times: %v %v", tr.Started, tr.Updated)
	}
}

func TestTranscriptProjection(t *testing.T) {
	fs := frames(
		"state", `{"state":"import","count":9,"from":"2026-09-05T13:00:00Z","to":"2026-09-05T13:05:00Z"}`,
		"transcript", `{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"hi"}}`,
		"transcript", `{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":10,"cache_read_input_tokens":90},"content":[{"type":"text","text":"Hello"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		"transcript", `{"type":"user","uuid":"u2","parentUuid":"a1","toolUseResult":{"stdout":"a\nb"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a\nb"}]}}`,
		"transcript", `{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"model":"claude-haiku-4-5","content":[{"type":"text","text":"Done"}]}}`,
		"transcript", `{"type":"system","uuid":"d1","parentUuid":"a2","subtype":"turn_duration","durationMs":1200}`,
		"transcript", `{"type":"user","uuid":"m1","parentUuid":"d1","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"}}`,
		"transcript", `{"type":"user","uuid":"u3","parentUuid":"m1","message":{"role":"user","content":"<command-name>/compact</command-name>\n<command-message>compact</command-message>\n<command-args></command-args>"}}`,
		"transcript", `{"type":"system","uuid":"l1","parentUuid":"u3","subtype":"local_command","content":"<local-command-stdout>Not enough messages to compact.</local-command-stdout>"}`,
		"transcript", `{"type":"user","uuid":"u4","parentUuid":"l1","message":{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}}`,
		"transcript", `{"type":"assistant","uuid":"a3","parentUuid":"u4","message":{"model":"claude-haiku-4-5","content":[{"type":"text","text":"I see it"}]}}`,
		"state", `{"state":"spawned","resumed":true}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "session.catchup input user.message assistant usage.context tool.result→tool:t1 assistant turn.end turn.idle fold input fold→cmd:8 fold→cmd:8 turn.idle input user.message assistant turn.end turn.idle session.spawned turn.idle"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	for _, e := range evs {
		switch {
		case e.T == model.SessionCatchup:
			if e.Data["source"] != "import" || e.Data["count"] != 9 {
				t.Errorf("catchup data: %v", e.Data)
			}
		case e.T == model.Input && e.Seq == 8:
			if c, _ := e.Data["cmd"].(map[string]any); c == nil || c["name"] != "compact" {
				t.Errorf("command prompt not recognised: %v", e.Data)
			}
		case e.T == model.Input && e.Seq == 10:
			if e.Data["text"] != "look" || len(e.Data["images"].([]any)) != 1 {
				t.Errorf("prompt with image: %v", e.Data)
			}
		case e.T == model.TurnEnd && e.Seq == 6:
			if e.Data["ok"] != true || e.Data["duration_ms"] != int64(1200) {
				t.Errorf("turn.end from turn_duration: %v", e.Data)
			}
		case e.T == model.UserMessage && e.Seq == 2:
			if e.Data["id"] != "u1" || e.Data["steer"] != false {
				t.Errorf("echo of the prompt: %v", e.Data)
			}
		case e.T == model.ToolResult:
			if e.Data["name"] != "Bash" || e.Data["text"] != "a\nb" {
				t.Errorf("tool.result: %v", e.Data)
			}
		}
	}
	// every raw shown is the record as it was on disk, never the synthesised frame
	for _, e := range evs {
		for _, r := range e.Raw {
			if r.Kind != "transcript" && r.Kind != "state" {
				t.Errorf("event %s shows a synthesised %s frame as evidence", e.T, r.Kind)
			}
		}
	}
}

func TestTranscriptCompaction(t *testing.T) {
	fs := frames(
		"state", `{"state":"catchup","count":4}`,
		"transcript", `{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"hi"}}`,
		"transcript", `{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"model":"m","content":[{"type":"text","text":"Hello"}]}}`,
		"transcript", `{"type":"system","uuid":"b1","parentUuid":null,"logicalParentUuid":"a1","subtype":"compact_boundary","compactMetadata":{"trigger":"auto","preTokens":79212,"postTokens":6130,"durationMs":100426}}`,
		"transcript", `{"type":"user","uuid":"s1","parentUuid":"b1","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued…"}}`,
		"transcript", `{"type":"user","uuid":"u2","parentUuid":"s1","message":{"role":"user","content":"go on"}}`,
		"state", `{"state":"exit","code":0}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	// the turn the compaction cut through closes into the compaction block, as
	// a live one does: its empty result is the compaction's own
	want := "session.catchup input user.message assistant compact.start compact.end→compact:4 usage.context compact.summary→compact:4 fold→compact:4 turn.idle input user.message turn.end turn.idle session.exit turn.idle"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	for _, e := range evs {
		if e.T == model.CompactEnd && (e.Data["pre"] != int64(79212) || e.Data["post"] != int64(6130) || e.Data["trigger"] != "auto") {
			t.Errorf("compact.end: %v", e.Data)
		}
		if e.T == model.CompactSummary && !strings.HasPrefix(e.Data["text"].(string), "This session") {
			t.Errorf("compact.summary: %v", e.Data)
		}
	}
}

func TestTranscriptTaskNotificationEndsLane(t *testing.T) {
	fs := frames(
		"transcript", `{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"run it in the background"}}`,
		"transcript", `{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"model":"m","content":[{"type":"tool_use","id":"call1","name":"Agent","input":{"description":"Smoke test","prompt":"do it","subagent_type":"Explore","run_in_background":true}}]}}`,
		"transcript", `{"type":"user","uuid":"u2","parentUuid":"a1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"Async agent launched successfully. agentId: abc"}]}}`,
		"transcript", `{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"model":"m","content":[{"type":"text","text":"Launched."}]}}`,
		"transcript", `{"type":"system","uuid":"d1","parentUuid":"a2","subtype":"turn_duration","durationMs":100}`,
		"transcript", `{"type":"user","uuid":"n1","parentUuid":"d1","message":{"role":"user","content":"<task-notification>\n<task-id>abc</task-id>\n<tool-use-id>call1</tool-use-id>\n<status>completed</status>\n<summary>All good</summary>\n</task-notification>"}}`,
		"transcript", `{"type":"assistant","uuid":"a3","parentUuid":"n1","message":{"model":"m","content":[{"type":"text","text":"It finished."}]}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	var ended bool
	for _, e := range evs {
		if e.T == model.AgentEnd {
			ended = true
			if e.Data["status"] != "completed" || e.Data["summary"] != "All good" || e.Data["id"] != "call1" {
				t.Errorf("agent.end: %v", e.Data)
			}
		}
		if e.T == model.UserMessage && strings.HasPrefix(e.Data["text"].(string), "<task-notification>") {
			t.Errorf("the notice rendered as a message")
		}
	}
	if !ended {
		t.Errorf("no agent.end: %s", kinds(evs))
	}
}
