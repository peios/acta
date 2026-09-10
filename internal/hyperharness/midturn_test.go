package hyperharness

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"github.com/google/uuid"
)

type sendProbePipe struct {
	harnesspipe.API
	status, turn, outcome string
	writes                map[string]harnesspipe.WriteRequest
	frames                []harnesspipe.RawFrame
	afterSend             func()
}

func (p *sendProbePipe) Write(ctx context.Context, q harnesspipe.WriteRequest) error {
	if prev, ok := p.writes[q.ID]; ok {
		if prev.Data != q.Data {
			return errors.New("changed input on recovery")
		}
		return nil
	}
	p.writes[q.ID] = q
	var m map[string]any
	_ = json.Unmarshal([]byte(q.Data), &m)
	params, _ := m["params"].(map[string]any)
	var response any
	switch m["method"] {
	case "thread/read":
		turns := []any{}
		if p.turn != "" {
			turns = append(turns, map[string]any{"id": p.turn, "status": "inProgress"})
		}
		response = map[string]any{"id": m["id"], "result": map[string]any{"thread": map[string]any{"id": params["threadId"], "status": map[string]any{"type": p.status}, "turns": turns}}}
	case "turn/start", "turn/steer":
		result := map[string]any{"turn": map[string]any{"id": "new-turn"}}
		if m["method"] == "turn/steer" {
			result = map[string]any{"turnId": params["expectedTurnId"]}
		}
		response = map[string]any{"id": m["id"], "result": result}
		if p.outcome == "reject" {
			response = map[string]any{"id": m["id"], "error": map[string]any{"message": "no active turn"}}
		}
		if p.outcome == "malformed" {
			response = map[string]any{"id": m["id"], "result": map[string]any{}}
		}
	default:
		// Claude accepts input while busy; no idle handshake should occur.
		response = map[string]any{"type": "command_lifecycle", "session_id": m["session_id"], "command_uuid": m["uuid"], "state": "queued"}
	}
	raw, _ := json.Marshal(response)
	p.frames = append(p.frames, harnesspipe.RawFrame{Sequence: int64(len(p.frames) + 1), RunID: q.RunID, Stream: "stdout", Data: raw, ReceivedAt: time.Now()})
	if (m["method"] == "turn/start" || m["method"] == "turn/steer" || m["type"] == "user") && p.afterSend != nil {
		p.afterSend()
	}
	return nil
}
func (p *sendProbePipe) Read(ctx context.Context, q harnesspipe.ReadRequest) ([]harnesspipe.RawFrame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []harnesspipe.RawFrame
	for _, f := range p.frames {
		if f.Sequence > q.After {
			out = append(out, f)
		}
	}
	return out, nil
}
func sendProbe(t *testing.T, provider, status, turn string) (*Controller, *sendProbePipe, *localThread, threads.Control) {
	t.Helper()
	p := &sendProbePipe{status: status, turn: turn, writes: map[string]harnesspipe.WriteRequest{}}
	c, err := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	id, run, command := uuid.NewString(), uuid.NewString(), uuid.NewString()
	q := threads.Control{ID: command, ThreadID: id, RunID: run, Action: "send", Text: "Please change direction"}
	r := &localThread{Thread: threads.Descriptor{ID: id, RunID: run, Provider: provider, ProviderID: "native", State: "running"}, Action: "send", CommandID: command, Requests: map[string]threads.Control{command: q}, Commands: map[string]threads.Result{}}
	c.records[id] = r
	return c, p, r, q
}
func TestMidTurnSendRouting(t *testing.T) {
	for _, tc := range []struct{ provider, status, turn, outcome, method string }{
		{"codex", "idle", "", "accepted", "turn/start"},
		{"codex", "active", "current", "accepted", "turn/steer"},
		{"codex", "active", "", "rejected", ""},
		{"claude", "running", "current", "accepted", "user"},
	} {
		t.Run(tc.provider+tc.status+tc.turn, func(t *testing.T) {
			c, p, r, q := sendProbe(t, tc.provider, tc.status, tc.turn)
			copy := *r
			c.executeSend(c.ctx, &copy)
			if got := r.Commands[q.ID]; got.Outcome != tc.outcome {
				t.Fatal(got)
			}
			found := false
			for _, w := range p.writes {
				var m map[string]any
				json.Unmarshal([]byte(w.Data), &m)
				if m["method"] == tc.method || m["type"] == tc.method {
					found = true
					if tc.method == "turn/steer" {
						params := m["params"].(map[string]any)
						if params["expectedTurnId"] != "current" || params["clientUserMessageId"] != q.ID {
							t.Fatal(params)
						}
					}
				}
			}
			if (tc.method != "") != found {
				t.Fatal("wrong send method", p.writes)
			}
		})
	}
}
func TestMidTurnSendRejectAndUncertain(t *testing.T) {
	for _, tc := range []struct{ reply, want string }{{"reject", "rejected"}, {"malformed", "uncertain"}} {
		t.Run(tc.reply, func(t *testing.T) {
			c, p, r, q := sendProbe(t, "codex", "active", "old-turn")
			p.outcome = tc.reply
			copy := *r
			c.executeSend(c.ctx, &copy)
			if got := r.Commands[q.ID]; got.Outcome != tc.want {
				t.Fatal(got)
			}
			if len(p.writes) != 3 {
				t.Fatal("unexpected retry or fallback", len(p.writes))
			}
		})
	}
}
func TestMidTurnSendRecoveryKeepsOriginalTurn(t *testing.T) {
	c, p, r, q := sendProbe(t, "codex", "active", "original")
	p.afterSend = c.cancel
	copy := *r
	c.executeSend(c.ctx, &copy)
	if r.Action != "send" || !r.SendAttempted {
		t.Fatal("lost pending send")
	}
	dir := c.dir
	c.Close()
	p.status = "active"
	p.turn = "later"
	p.afterSend = nil
	next, err := NewController(t.Context(), dir, p)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	recovered := *next.records[r.Thread.ID]
	next.executeSend(next.ctx, &recovered)
	if got := next.records[r.Thread.ID].Commands[q.ID]; got.Outcome != "accepted" {
		t.Fatal(got)
	}
	if len(p.writes) != 3 {
		t.Fatal("recovery sent more than original reads and steer", len(p.writes))
	}
}
