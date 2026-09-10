package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const thread = "11111111-1111-4111-8111-111111111111"
const run = "22222222-2222-4222-8222-222222222222"

func capture(sequence int64) threads.ProviderFrame {
	return threads.ProviderFrame{ThreadID: thread, RunID: run, Sequence: sequence, Provider: "codex", ReceivedAt: time.Date(2026, 9, 7, 17, 0, 0, 0, time.UTC)}
}
func TestCodexReviewedSequence(t *testing.T) {
	cases := []struct{ raw, kind string }{
		{`{"id":"RUN/initialize","result":{"userAgent":"Codex/1"}}`, "debug/local"},
		{`{"method":"remoteControl/status/changed","params":{"status":"disabled"}}`, "debug/dropped"},
		{`{"method":"guardianWarning","params":{"threadId":"native","message":"Automatic approval review approved."}}`, "debug/dropped"},
		{`{"id":"RUN/thread/start","result":{"thread":{"id":"native"},"model":"model-x","cwd":"/tmp","serviceTier":"default","approvalPolicy":"on-request","approvalsReviewer":"auto_review","sandbox":{"type":"workspaceWrite","networkAccess":false,"writableRoots":[]},"reasoningEffort":"high"}}`, "thread/configuration"},
		{`{"method":"thread/started","params":{"thread":{"id":"native"}}}`, "debug/local"},
		{`{"method":"mcpServer/startupStatus/updated","params":{"threadId":"native","name":"acta2","status":"starting","error":null,"failureReason":null}}`, "mcp/server/status"},
		{`{"id":"RUN/turn/start","result":{"turn":{"id":"turn1","status":"inProgress"}}}`, "turn/started"},
		{`{"method":"turn/started","params":{"threadId":"native","turn":{"id":"turn1"}}}`, "debug/dropped"},
		{`{"id":"RUN/turn/steer/submission","result":{"turnId":"turn1"}}`, "debug/local"},
		{`{"method":"thread/status/changed","params":{"threadId":"native","status":{"type":"active","activeFlags":["waitingOnApproval"]}}}`, "thread/status"},
		{`{"method":"item/started","params":{"threadId":"native","turnId":"turn1","item":{"id":"u1","type":"userMessage","content":[{"type":"text","text":"Test Message","text_elements":[]}]},"startedAtMs":1788800429146}}`, "message/user"},
		{`{"method":"item/completed","params":{"threadId":"native","turnId":"turn1","item":{"id":"u1","type":"userMessage","content":[{"type":"text","text":"Test Message","text_elements":[]}]},"completedAtMs":1788800429147}}`, "message/user"},
		{`{"method":"item/started","params":{"threadId":"native","turnId":"turn1","item":{"id":"a1","type":"agentMessage","text":"","phase":"final_answer","memoryCitation":null},"startedAtMs":1788800431524}}`, "message/assistant"},
		{`{"method":"item/agentMessage/delta","params":{"threadId":"native","turnId":"turn1","itemId":"a1","delta":"Message"}}`, "message/assistant/delta"},
		{`{"method":"item/agentMessage/delta","params":{"threadId":"native","turnId":"turn1","itemId":"a1","delta":"Message"}}`, "message/assistant/delta"},
		{`{"method":"item/completed","params":{"threadId":"native","turnId":"turn1","item":{"id":"a1","type":"agentMessage","text":"Message received.","phase":"final_answer"},"completedAtMs":1788800431668}}`, "message/assistant"},
		{`{"method":"item/agentMessage/delta","params":{"threadId":"native","turnId":"turn1","itemId":"a1","delta":"late"}}`, "debug/dropped"},
		{`{"method":"thread/tokenUsage/updated","params":{"threadId":"native","turnId":"turn1","tokenUsage":{"total":{"totalTokens":20908,"inputTokens":20901,"outputTokens":7},"last":{"totalTokens":20908},"modelContextWindow":258400}}}`, "usage/context"},
		{`{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":80,"windowDurationMins":10080,"resetsAt":1789328280},"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"planType":"pro"}}}`, "usage/account"},
		{`{"method":"turn/completed","params":{"threadId":"native","turn":{"id":"turn1","status":"completed","error":null,"startedAt":1788800427,"completedAt":1788800431,"durationMs":4364,"items":[]}}}`, "turn/completed"},
		{`{"method":"thread/status/changed","params":{"threadId":"native","status":{"type":"idle"}}}`, "thread/status"},
		{`{"id":"RUN/thread/resume","result":{"thread":{"id":"native","turns":[{"id":"turn1","items":[{"type":"agentMessage","id":"item-2","text":"Message received."}]}]},"model":"model-y","cwd":"/tmp"}}`, "thread/configuration"},
	}
	state := State{NativeID: "native"}
	for i, c := range cases {
		raw := strings.ReplaceAll(c.raw, "RUN", run)
		next, bundle, err := Map(state, capture(int64(i+1)), "stdout", raw)
		if err != nil {
			t.Fatal(i, err)
		}
		if bundle[len(bundle)-1].Kind != c.kind {
			t.Fatalf("%d: wanted %s got %s %s", i, c.kind, bundle[len(bundle)-1].Kind, string(bundle[0].Data))
		}
		var debug object
		_ = json.Unmarshal(bundle[0].Data, &debug)
		if debug["raw"] != raw {
			t.Fatal("raw changed")
		}
		if len(bundle) > 1 && bundle[0].Kind != "debug/resolved" {
			t.Fatal("missing resolution")
		}
		if err = threads.ValidateBundle(bundle); err != nil {
			t.Fatal(i, err)
		}
		// State survives the exact persistence boundary used between controller polls.
		encoded, _ := json.Marshal(next)
		state, err = DecodeState(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestUnsupportedAndDiagnosticEvidence(t *testing.T) {
	for _, c := range []struct{ raw, stream, kind string }{
		{`{ "method": "future/event", "value": 9007199254740993 }`, "stdout", "debug/unknown"},
		{`{"method":"item/started","params":{"threadId":"native","turnId":"t","item":{"id":"u","type":"userMessage","content":[{"type":"image","url":"example"}]}}}`, "stdout", "debug/unknown"},
		{`{"method":"thread/status/changed","params":{"threadId":"another","status":{"type":"idle"}}}`, "stdout", "debug/unknown"},
		{`{"method":"mcpServer/startupStatus/updated","params":{"threadId":"native","name":"x","status":"nonsense"}}`, "stdout", "debug/local"},
		{`{"id":"RUN/thread/read/1","error":{"message":"not saved yet"}}`, "stdout", "debug/local"},
		{`{"level":"WARN","message":"exact stderr"}`, "stderr", "debug/provider-diagnostic"},
	} {
		_, bundle, err := Map(State{NativeID: "native"}, capture(1), c.stream, strings.ReplaceAll(c.raw, "RUN", run))
		if err != nil {
			t.Fatal(err)
		}
		if len(bundle) != 1 || bundle[0].Kind != c.kind {
			t.Fatal(c, bundle)
		}
	}
}
func TestPermissionMappings(t *testing.T) {
	for _, c := range []struct{ policy, reviewer, sandbox, want string }{{"on-request", "user", "workspaceWrite", "ask"}, {"on-request", "auto_review", "workspaceWrite", "automatic"}, {"untrusted", "user", "workspaceWrite", "accept_edits"}, {"never", "user", "workspaceWrite", "dont_ask"}, {"never", "user", "dangerFullAccess", "bypass"}} {
		p := permissions(object{"approvalPolicy": c.policy, "approvalsReviewer": c.reviewer, "sandbox": object{"type": c.sandbox}})
		if p["mode"] != c.want {
			t.Fatal(c, p)
		}
	}
}

func TestSettingsNotificationNormalizesAndPreservesPermissions(t *testing.T) {
	state := State{NativeID: "native"}
	raw := `{"method":"thread/settings/updated","params":{"threadId":"native","threadSettings":{"model":"test-model","effort":"high","serviceTier":"priority","cwd":"/tmp","approvalPolicy":"on-request","approvalsReviewer":"auto_review","sandboxPolicy":{"type":"workspaceWrite","networkAccess":false,"writableRoots":[]}}}}`
	next, frames, err := Map(state, capture(1), "stdout", raw)
	if err != nil || len(frames) != 2 || frames[1].Kind != "thread/configuration" {
		t.Fatal(frames, err)
	}
	if next.Configuration["fast_mode"] != true || next.Configuration["effort"] != "high" || obj(next.Configuration["permissions"])["mode"] != "automatic" {
		t.Fatal(next.Configuration)
	}
	_, duplicate, err := Map(next, capture(2), "stdout", raw)
	if err != nil || len(duplicate) != 1 || duplicate[0].Kind != "debug/local" {
		t.Fatal("duplicate config", duplicate, err)
	}
	_, foreign, err := Map(next, capture(3), "stdout", strings.Replace(raw, `"threadId":"native"`, `"threadId":"foreign"`, 1))
	if err != nil || len(foreign) != 1 || foreign[0].Kind != "debug/unknown" {
		t.Fatal(foreign, err)
	}
}

func TestCodexEmptyTerminalPollIsDropped(t *testing.T) {
	for _, tc := range []struct {
		name, input, native, tool string
		want                      string
	}{
		{"poll", "", "native", "shell", "debug/dropped"},
		{"input", "yes\n", "native", "shell", "debug/unknown"},
		{"wrong thread", "", "other", "shell", "debug/unknown"},
		{"missing thread", "", "", "shell", "debug/unknown"},
		{"unknown tool", "", "native", "absent", "debug/unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := State{RunID: run, NativeID: "native", Tools: map[string]*ToolCall{"shell": {ID: "shell", Turn: "turn", Name: "Shell", Status: "running"}}}
			raw, _ := json.Marshal(object{"method": "item/commandExecution/terminalInteraction", "params": object{"threadId": tc.native, "turnId": "turn", "itemId": tc.tool, "processId": "123", "stdin": tc.input}})
			_, frames, e := Map(s, capture(1), "stdout", string(raw))
			if e != nil {
				t.Fatal(e)
			}
			if len(frames) != 1 || frames[0].Kind != tc.want {
				t.Fatalf("%+v", frames)
			}
		})
	}
}
