package hyperharness

import (
	"acta/internal/harnesspipe"
	"acta/internal/threadadapter"
	"acta/internal/threads"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type approvalPipe struct {
	framePipe
	mu         sync.Mutex
	writes     map[string]string
	afterWrite func()
}

func (p *approvalPipe) Write(_ context.Context, q harnesspipe.WriteRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if prior, ok := p.writes[q.ID]; ok && prior != q.Data {
		return errors.New("conflict")
	}
	p.writes[q.ID] = q.Data
	if p.afterWrite != nil {
		p.afterWrite()
	}
	return nil
}
func TestApprovalDecisionDurableAndRunBound(t *testing.T) {
	run := uuid.NewString()
	raw := json.RawMessage(`{"id":"request","method":"item/commandExecution/requestApproval","params":{"threadId":"native","turnId":"turn","itemId":"tool","command":"echo yes"}}`)
	pipe := &approvalPipe{framePipe: framePipe{frames: []harnesspipe.RawFrame{{RunID: run, Sequence: 1, Stream: "stdout", Data: raw, ReceivedAt: time.Now()}}}, writes: map[string]string{}}
	dir := filepath.Join(t.TempDir(), "controller")
	c, err := NewController(t.Context(), dir, pipe)
	if err != nil {
		t.Fatal(err)
	}
	a := threadadapter.ParseApproval("codex", run, "native", raw)
	c.records[run] = &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: "codex", ProviderID: "native", State: "running"}, Commands: map[string]threads.Result{}, Requests: map[string]threads.Control{}}
	q := threads.Control{ID: a.ID, ApprovalID: a.ID, ThreadID: run, RunID: run, Action: "approval", Decision: "approve"}
	if err = c.Control(q); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := c.records[run].Commands[q.ID].Outcome == "accepted"
		c.mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	c.mu.Lock()
	result := c.records[run].Commands[q.ID]
	c.mu.Unlock()
	if result.Outcome != "accepted" {
		t.Fatal(result)
	}
	c.Close()
	c, err = NewController(t.Context(), dir, pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Control(q); err != nil {
		t.Fatal(err)
	}
	q.Decision = "deny"
	if err = c.Control(q); err == nil {
		t.Fatal("opposite decision accepted")
	}
	q.Decision = "approve"
	q.RunID = uuid.NewString()
	q.ID = uuid.NewString()
	q.ApprovalID = q.ID
	if err = c.Control(q); err == nil {
		t.Fatal("stale run accepted")
	}
	pipe.mu.Lock()
	defer pipe.mu.Unlock()
	if len(pipe.writes) != 1 {
		t.Fatal("duplicate write", pipe.writes)
	}
}
func TestResolvedApprovalCannotBeAnswered(t *testing.T) {
	run := uuid.NewString()
	raw := json.RawMessage(`{"id":"request","method":"item/fileChange/requestApproval","params":{"threadId":"native","turnId":"t","itemId":"i"}}`)
	pipe := &framePipe{frames: []harnesspipe.RawFrame{{RunID: run, Sequence: 1, Stream: "stdout", Data: raw}, {RunID: run, Sequence: 2, Stream: "stdout", Data: json.RawMessage(`{"method":"serverRequest/resolved","params":{"threadId":"native","requestId":"request"}}`)}}}
	c, err := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: "codex", ProviderID: "native"}}
	a := threadadapter.ParseApproval("codex", run, "native", raw)
	if _, err = c.pendingApproval(t.Context(), r, a.ID); err == nil {
		t.Fatal("resolved request still actionable")
	}
}

func TestApprovalRecoversWriteBeforeResult(t *testing.T) {
	run := uuid.NewString()
	raw := json.RawMessage(`{"id":"request","method":"item/fileChange/requestApproval","params":{"threadId":"native","turnId":"t","itemId":"i"}}`)
	pipe := &approvalPipe{framePipe: framePipe{frames: []harnesspipe.RawFrame{{RunID: run, Sequence: 1, Stream: "stdout", Data: raw}}}, writes: map[string]string{}}
	dir := filepath.Join(t.TempDir(), "controller")
	c, err := NewController(t.Context(), dir, pipe)
	if err != nil {
		t.Fatal(err)
	}
	a := threadadapter.ParseApproval("codex", run, "native", raw)
	q := threads.Control{ID: a.ID, ApprovalID: a.ID, ThreadID: run, RunID: run, Action: "approval", Decision: "deny"}
	c.records[run] = &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: "codex", ProviderID: "native", State: "running"}, Commands: map[string]threads.Result{}, Requests: map[string]threads.Control{}}
	pipe.afterWrite = c.cancel
	if err = c.Control(q); err != nil {
		t.Fatal(err)
	}
	c.wg.Wait()
	c.Close()
	// Native request has now resolved, but the exact written reply is journaled.
	pipe.frames = append(pipe.frames, harnesspipe.RawFrame{RunID: run, Sequence: 2, Stream: "stdout", Data: json.RawMessage(`{"method":"serverRequest/resolved","params":{"threadId":"native","requestId":"request"}}`)})
	pipe.afterWrite = nil
	c, err = NewController(t.Context(), dir, pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.mu.Lock()
	c.launch(run)
	c.mu.Unlock()
	c.wg.Wait()
	if got := c.records[run].Commands[q.ID]; got.Outcome != "accepted" || got.Decision != "deny" {
		t.Fatal(got)
	}
	if len(pipe.writes) != 1 {
		t.Fatal("replayed decision wrote different provider input")
	}
}

func TestCodexPermissionConfirmationIncludesUnchangedResume(t *testing.T) {
	const run = "current-run"
	resume := func(requestRun, native, reviewer string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"id": requestRun + "/thread/resume", "result": map[string]any{"thread": map[string]string{"id": native}, "approvalPolicy": "on-request", "approvalsReviewer": reviewer, "sandbox": map[string]string{"type": "workspaceWrite"}}})
		return raw
	}
	notice := func(reviewer string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"method": "thread/settings/updated", "params": map[string]any{"threadId": "native", "threadSettings": map[string]any{"approvalPolicy": "on-request", "approvalsReviewer": reviewer, "sandboxPolicy": map[string]string{"type": "workspaceWrite"}}}})
		return raw
	}
	for _, tc := range []struct {
		name         string
		observations []json.RawMessage
		accepted     bool
	}{
		{"unchanged resume has no change notification", []json.RawMessage{resume(run, "native", "auto_review")}, true},
		{"changed settings notification", []json.RawMessage{resume(run, "native", "user"), notice("auto_review")}, true},
		{"ack alone is insufficient", nil, false},
		{"other run cannot confirm", []json.RawMessage{resume("old-run", "native", "auto_review")}, false},
		{"other native thread cannot confirm", []json.RawMessage{resume(run, "other", "auto_review")}, false},
		{"later mismatch overrides matching resume", []json.RawMessage{resume(run, "native", "auto_review"), notice("user")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := []harnesspipe.RawFrame{}
			for n, raw := range tc.observations {
				frames = append(frames, harnesspipe.RawFrame{RunID: run, Sequence: int64(n + 1), Stream: "stdout", Data: raw})
			}
			ack, _ := json.Marshal(map[string]any{"id": run + "/thread/settings/update/permissions-resume-permissions", "result": map[string]any{}})
			frames = append(frames, harnesspipe.RawFrame{RunID: run, Sequence: int64(len(frames) + 1), Stream: "stdout", Data: ack})
			pipe := &approvalPipe{framePipe: framePipe{frames: frames}, writes: map[string]string{}}
			c := &Controller{pipe: pipe}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			err := c.applyPermissions(ctx, &localThread{Thread: threads.Descriptor{ID: "acta-thread", RunID: run, Provider: "codex", ProviderID: "native"}}, "resume-permissions", "automatic")
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, error=%v", tc.accepted, err)
			}
		})
	}
}

func TestChildApprovalSurvivesParentCompletion(t *testing.T) {
	run := uuid.NewString()
	native := uuid.NewString()
	raw := json.RawMessage(`{"type":"control_request","request_id":"child-request","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":"write","agent_id":"child","input":{"file_path":"/tmp/qa","content":"qa"}}}`)
	pipe := &framePipe{frames: []harnesspipe.RawFrame{{RunID: run, Sequence: 1, Stream: "stdout", Data: raw}, {RunID: run, Sequence: 2, Stream: "stdout", Data: json.RawMessage(`{"type":"result","session_id":"` + native + `","subtype":"success"}`)}}}
	c, e := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), pipe)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	r := &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: "claude", ProviderID: native}, Adapter: threadadapter.State{NativeID: native, Lanes: map[string]*threadadapter.Lane{"child": {ID: "child", Native: native, State: threadadapter.State{RunID: run}}}}}
	a := threadadapter.ParseApproval("claude", run, native, raw)
	if a == nil {
		t.Fatal("fixture not approval")
	}
	if _, e = c.pendingApproval(t.Context(), r, a.ID); e != nil {
		t.Fatal("parent result cancelled child approval", e)
	}
}

type livePermissionPipe struct {
	approvalPipe
	run string
}

func (p *livePermissionPipe) Processes(context.Context) ([]harnesspipe.Process, error) {
	return []harnesspipe.Process{{Spec: harnesspipe.Spec{RunID: p.run, Args: []string{"--permission-prompt-tool", "stdio"}}, State: "running"}}, nil
}

func TestPermissionChangeDuringApproval(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		for _, outcome := range []string{"accepted", "rejected", "uncertain"} {
			t.Run(provider+"/"+outcome, func(t *testing.T) {
				run, id := uuid.NewString(), uuid.NewString()
				p := &livePermissionPipe{approvalPipe: approvalPipe{writes: map[string]string{}}, run: run}
				add := func(value any) {
					raw, _ := json.Marshal(value)
					p.frames = append(p.frames, harnesspipe.RawFrame{RunID: run, Sequence: int64(len(p.frames) + 1), Stream: "stdout", Data: raw})
				}
				if provider == "codex" {
					add(map[string]any{"method": "thread/status/changed", "params": map[string]any{"threadId": "native", "status": map[string]any{"type": "active", "activeFlags": []string{"waitingOnApproval"}}}})
					add(map[string]any{"id": "pending", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "native", "turnId": "turn", "itemId": "tool", "command": "sleep 15"}})
					reply := map[string]any{"id": run + "/thread/settings/update/permissions-" + id, "result": map[string]any{}}
					if outcome == "rejected" {
						delete(reply, "result")
						reply["error"] = map[string]string{"message": "mode unavailable"}
					}
					add(reply)
					if outcome == "accepted" {
						add(map[string]any{"method": "thread/settings/updated", "params": map[string]any{"threadId": "native", "threadSettings": map[string]any{"approvalPolicy": "never", "approvalsReviewer": "user", "sandboxPolicy": map[string]string{"type": "dangerFullAccess"}}}})
					}
				} else {
					add(map[string]any{"type": "control_request", "request_id": "pending", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Write", "tool_use_id": "write", "input": map[string]string{"file_path": "/tmp/test", "content": "OK"}}})
					response := map[string]any{"request_id": run + "/set_permission_mode/" + id, "subtype": "success", "response": map[string]any{}}
					if outcome == "rejected" {
						response["subtype"] = "error"
						response["error"] = "mode unavailable"
					}
					add(map[string]any{"type": "control_response", "response": response})
					mode := "bypassPermissions"
					if outcome == "uncertain" {
						mode = "default"
					}
					add(map[string]any{"type": "control_response", "response": map[string]any{"request_id": run + "/initialize/permissions/" + id, "subtype": "success", "response": map[string]any{"current_permission_mode": mode, "session_state": "requires_action"}}})
				}
				c, err := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), p)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				q := threads.Control{ID: id, ThreadID: run, RunID: run, Action: "permissions", PermissionMode: "bypass"}
				r := &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: provider, ProviderID: "native", State: "running"}, Action: "permissions", CommandID: id, PermissionMode: "ask", Commands: map[string]threads.Result{}, Requests: map[string]threads.Control{id: q}}
				c.records[run] = r
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
				c.executePermissions(ctx, r)
				if got := r.Commands[id]; got.Outcome != outcome {
					t.Fatalf("got %+v", got)
				}
				want := "ask"
				if outcome == "accepted" {
					want = "bypass"
				}
				if r.PermissionMode != want {
					t.Fatal("stored unconfirmed policy", r.PermissionMode)
				}
				for _, raw := range p.writes {
					var m map[string]any
					_ = json.Unmarshal([]byte(raw), &m)
					if m["method"] == "thread/read" || m["type"] == "control_response" {
						t.Fatal("queried idle or answered approval", raw)
					}
					if request, ok := m["request"].(map[string]any); ok && request["subtype"] == "initialize" && m["request_id"] != run+"/initialize/permissions/"+id {
						t.Fatal("idle check sent", raw)
					}
				}
				raw := p.frames[0].Data
				if provider == "codex" {
					raw = p.frames[1].Data
				}
				approval := threadadapter.ParseApproval(provider, run, "native", raw)
				if approval == nil {
					t.Fatal("bad approval fixture")
				}
				if _, err := c.pendingApproval(t.Context(), r, approval.ID); err != nil {
					t.Fatal("mode change consumed pending approval", err)
				}
			})
		}
	}
}
