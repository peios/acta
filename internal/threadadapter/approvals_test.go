package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestApprovalMappingAndExactReplies(t *testing.T) {
	run := uuid.NewString()
	for _, tc := range []struct{ provider, raw, want string }{
		{"codex", `{"id":9007199254740993,"method":"item/commandExecution/requestApproval","params":{"threadId":"native","turnId":"turn","itemId":"tool","command":"echo hello","reason":"approval"}}`, `"id":9007199254740993`},
		{"codex", `{"id":"file-request","method":"item/fileChange/requestApproval","params":{"threadId":"native","turnId":"turn","itemId":"tool"}}`, `"decision":"accept"`},
		{"claude", `{"type":"control_request","request_id":"req","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":"tool","input":{"file_path":"/tmp/check","content":"unchanged"}}}`, `"updatedInput":{"content":"unchanged","file_path":"/tmp/check"}`},
	} {
		t.Run(tc.provider+tc.want, func(t *testing.T) {
			a := ParseApproval(tc.provider, run, "native", []byte(tc.raw))
			if a == nil {
				t.Fatal("missing approval")
			}
			if b := ParseApproval(tc.provider, run, "native", []byte(tc.raw)); b.ID != a.ID {
				t.Fatal("unstable request identity")
			}
			if b := ParseApproval(tc.provider, uuid.NewString(), "native", []byte(tc.raw)); b.ID == a.ID {
				t.Fatal("shared request identity across runs")
			}
			raw, err := a.Reply(true)
			if err != nil || !strings.Contains(string(raw), tc.want) {
				t.Fatal(string(raw), err)
			}
			raw, err = a.Reply(false)
			if err != nil || strings.Contains(string(raw), `"updatedInput"`) || strings.Contains(string(raw), `"accept"`) {
				t.Fatal(string(raw), err)
			}
			_, bundle, err := Map(State{NativeID: "native"}, threads.ProviderFrame{Provider: tc.provider, ThreadID: run, RunID: run, Sequence: 1, ReceivedAt: time.Now()}, "stdout", tc.raw)
			if err != nil || len(bundle) != 2 || bundle[1].Kind != "approval/request" {
				t.Fatal(bundle, err)
			}
			var d map[string]any
			_ = json.Unmarshal(bundle[1].Data, &d)
			if d["approval_id"] != a.ID {
				t.Fatal(d)
			}
		})
	}
}
func TestApprovalWrongIdentityAndResolution(t *testing.T) {
	run := uuid.NewString()
	raw := []byte(`{"id":"q","method":"item/fileChange/requestApproval","params":{"threadId":"native","turnId":"t","itemId":"i"}}`)
	if ParseApproval("codex", run, "other", raw) != nil {
		t.Fatal("accepted foreign thread")
	}
	a := ParseApproval("codex", run, "native", raw)
	id := ApprovalResolved("codex", run, "native", []byte(`{"method":"serverRequest/resolved","params":{"threadId":"native","requestId":"q"}}`))
	if a.ID != id {
		t.Fatal("resolution mismatch")
	}
}

func TestClaudeApprovalResponseEcho(t *testing.T) {
	run := uuid.NewString()
	a := ParseApproval("claude", run, "native", []byte(`{"type":"control_request","request_id":"request","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"/tmp/example","content":"example"}}}`))
	for _, allow := range []bool{true, false} {
		raw, err := a.Reply(allow)
		if err != nil {
			t.Fatal(err)
		}
		if got := ApprovalResolved("claude", run, "native", raw); got != a.ID {
			t.Fatalf("wrong resolution: %s", got)
		}
		_, bundle, err := Map(State{NativeID: "native"}, threads.ProviderFrame{Provider: "claude", ThreadID: run, RunID: run, Sequence: 2, ReceivedAt: time.Now()}, "stdout", string(raw))
		if err != nil || len(bundle) != 2 || bundle[0].Kind != "debug/resolved" || bundle[1].Kind != "approval/resolved" {
			t.Fatal(bundle, err)
		}
	}
	for _, raw := range []string{
		`{"type":"control_response","response":{"subtype":"success","request_id":"request","response":{}}}`,
		`{"type":"control_response","response":{"subtype":"error","request_id":"request","response":{"behavior":"deny"}}}`,
		`{"type":"control_response","session_id":"foreign","response":{"subtype":"success","request_id":"request","response":{"behavior":"allow"}}}`,
	} {
		if got := ApprovalResolved("claude", run, "native", []byte(raw)); got != "" {
			t.Fatalf("resolved unrelated response: %s", raw)
		}
	}
}
