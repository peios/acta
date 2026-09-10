package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"path/filepath"
	"testing"
	"time"
)

func TestInterruptProviderProtocolAndReplay(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			run, id := uuid.NewString(), uuid.NewString()
			pipe := &approvalPipe{writes: map[string]string{}}
			appendFrame := func(raw string) {
				pipe.frames = append(pipe.frames, harnesspipe.RawFrame{RunID: run, Stream: "stdout", Sequence: int64(len(pipe.frames) + 1), Data: json.RawMessage(raw)})
			}
			if provider == "codex" {
				appendFrame(`{"id":"` + run + `/thread/read/interrupt-` + id + `","result":{"thread":{"id":"native","turns":[{"id":"old","status":"completed"},{"id":"active","status":"inProgress"}]}}}`)
				appendFrame(`{"id":"` + run + `/turn/interrupt/` + id + `","result":{}}`)
			} else {
				appendFrame(`{"type":"control_response","response":{"request_id":"` + run + `/interrupt/` + id + `","subtype":"success","response":{}}}`)
			}
			c, err := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), pipe)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.records[run] = &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: provider, ProviderID: "native", State: "running"}, Commands: map[string]threads.Result{}, Requests: map[string]threads.Control{}}
			q := threads.Control{ID: id, ThreadID: run, RunID: run, Action: "interrupt"}
			if err = c.Control(q); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				c.mu.Lock()
				done := c.records[run].Commands[id].Outcome != ""
				c.mu.Unlock()
				if done {
					break
				}
				time.Sleep(time.Millisecond)
			}
			c.mu.Lock()
			result := c.records[run].Commands[id]
			state := c.records[run].Thread.State
			c.mu.Unlock()
			if result.Outcome != "accepted" || state != "running" {
				t.Fatalf("%+v state=%s", result, state)
			}
			if err = c.Control(q); err != nil {
				t.Fatal(err)
			}
			q.ID = uuid.NewString()
			q.RunID = uuid.NewString()
			if c.Control(q) == nil {
				t.Fatal("stale run accepted")
			}
			pipe.mu.Lock()
			defer pipe.mu.Unlock()
			suffix := "interrupt/"
			if provider == "codex" {
				suffix = "turn/interrupt/"
			}
			var request map[string]any
			if err = json.Unmarshal([]byte(pipe.writes[run+"/"+suffix+id]), &request); err != nil {
				t.Fatal(err)
			}
			if provider == "codex" {
				p := request["params"].(map[string]any)
				if p["turnId"] != "active" || p["threadId"] != "native" {
					t.Fatal(request)
				}
			} else if request["request"].(map[string]any)["subtype"] != "interrupt" {
				t.Fatal(request)
			}
			want := 1
			if provider == "codex" {
				want = 2
			}
			if len(pipe.writes) != want {
				t.Fatal(pipe.writes)
			}
		})
	}
}
func TestInterruptDoesNotTargetCompletedOrOtherThread(t *testing.T) {
	for _, thread := range []string{`{"id":"native","turns":[{"id":"done","status":"completed"}]}`, `{"id":"other","turns":[{"id":"active","status":"inProgress"}]}`} {
		pipe := &approvalPipe{writes: map[string]string{}, framePipe: framePipe{frames: []harnesspipe.RawFrame{{RunID: "run", Sequence: 1, Stream: "stdout", Data: json.RawMessage(`{"id":"run/thread/read/interrupt-id","result":{"thread":` + thread + `}}`)}}}}
		c := &Controller{pipe: pipe}
		r := &localThread{Thread: threads.Descriptor{ID: "thread", RunID: "run", Provider: "codex", ProviderID: "native"}}
		if c.interrupt(context.Background(), r, "id") == nil {
			t.Fatal("unexpected success")
		}
		if len(pipe.writes) != 1 {
			t.Fatal("sent an interrupt", pipe.writes)
		}
	}
}
