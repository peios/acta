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

type framePipe struct {
	harnesspipe.API
	frames []harnesspipe.RawFrame
}

func (p *framePipe) Read(_ context.Context, q harnesspipe.ReadRequest) ([]harnesspipe.RawFrame, error) {
	out := []harnesspipe.RawFrame{}
	for _, f := range p.frames {
		if f.Sequence > q.After {
			out = append(out, f)
		}
	}
	return out, nil
}
func TestMappedBundleSurvivesRestartAndDiskFailure(t *testing.T) {
	id := uuid.NewString()
	native := `{"method":"thread/status/changed","params":{"threadId":"native","status":{"type":"idle"}}}`
	pipe := &framePipe{frames: []harnesspipe.RawFrame{{RunID: id, Sequence: 1, ReceivedAt: time.Now().UTC(), Stream: "stdout", Data: json.RawMessage(native)}}}
	dir := filepath.Join(t.TempDir(), "controller")
	c, err := NewController(t.Context(), dir, pipe)
	if err != nil {
		t.Fatal(err)
	}
	r := &localThread{Thread: threads.Descriptor{ID: id, RunID: id, Provider: "codex", ProviderID: "native"}, Discovered: true}
	c.records[id] = r
	if err = c.save(r); err != nil {
		t.Fatal(err)
	}
	frames, err := c.Frames(t.Context(), id)
	if err != nil || len(frames) != 2 {
		t.Fatal(frames, err)
	}
	before, _ := json.Marshal(frames)
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = NewController(t.Context(), dir, pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Simulate a later adapter seeing different source contents: durable outputs win.
	pipe.frames[0].Data = json.RawMessage(`{"changed":true}`)
	frames, err = c.Frames(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(frames)
	if string(before) != string(after) {
		t.Fatal("reinterpreted delivered capture")
	}
	c.persist = func(string, any) error { return errors.New("disk full") }
	if err = c.Acknowledge(id, 1); err == nil {
		t.Fatal("acknowledged without durable state")
	}
	if c.records[id].Ack != 0 || len(c.records[id].Pending) != 2 {
		t.Fatal("lost unacknowledged bundle")
	}
}
func TestSavedHistoryCommitmentCriterion(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want bool
	}{
		{`{"thread":{"id":"native","path":"/exists"}}`, false},
		{`{"thread":{"id":"native","turns":[{"itemsView":"full","items":[]}]}}`, false},
		{`{"thread":{"id":"native","turns":[{"itemsView":"summary","items":[{"type":"userMessage","content":[{}]}]}]}}`, false},
		{`{"thread":{"id":"wrong","turns":[{"itemsView":"full","items":[{"type":"userMessage","content":[{}]}]}]}}`, false},
		{`{"thread":{"id":"native","turns":[{"itemsView":"full","items":[{"type":"userMessage","content":[{"type":"text","text":"Test Message"}]}]}]}}`, true},
	} {
		if got := savedUserHistory(json.RawMessage(c.raw), "native"); got != c.want {
			t.Fatal(c, got)
		}
	}
}

func TestLateConfigurationReceiptRequiresFullRequestIdentity(t *testing.T) {
	for _, suffix := range []string{"bare-id", "wrong-prefix", "exact"} {
		t.Run(suffix, func(t *testing.T) {
			id, command := uuid.NewString(), uuid.NewString()
			replyID := command
			if suffix == "wrong-prefix" {
				replyID = id + "/other/" + command
			}
			if suffix == "exact" {
				replyID = id + "/thread/settings/update/" + command
			}
			raw, _ := json.Marshal(map[string]any{"id": replyID, "result": map[string]any{}})
			pipe := &framePipe{frames: []harnesspipe.RawFrame{{RunID: id, Sequence: 1, ReceivedAt: time.Now().UTC(), Stream: "stdout", Data: raw}}}
			c, err := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), pipe)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.records[id] = &localThread{Thread: threads.Descriptor{ID: id, RunID: id, Provider: "codex", ProviderID: "native"}, Discovered: true, Requests: map[string]threads.Control{command: {ID: command, Action: "configure", RunID: id}}, Commands: map[string]threads.Result{command: {ID: command, Outcome: "uncertain"}}}
			if _, err = c.Frames(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			got := c.records[id].Commands[command].Outcome
			if (got == "accepted") != (suffix == "exact") {
				t.Fatalf("%s receipt changed outcome to %s", suffix, got)
			}
		})
	}
}
