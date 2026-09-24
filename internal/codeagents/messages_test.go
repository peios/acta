package codeagents

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"acta/internal/codethreads"
	"github.com/google/uuid"
)

type messageWriter struct {
	writes chan []byte
	fail   bool
}

func (w messageWriter) Write(p []byte) (int, error) {
	if w.fail {
		return 0, errors.New("broken pipe")
	}
	w.writes <- append([]byte(nil), p...)
	return len(p), nil
}

func TestMessageAcknowledgementAndTurnCompletion(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			writes := make(chan []byte, 1)
			var frames []codethreads.Payload
			var mu sync.Mutex
			m := &messages{provider: provider, stdin: messageWriter{writes: writes}, processDone: make(chan struct{}), cancel: func() {}, emit: func(f codethreads.Payload) { mu.Lock(); frames = append(frames, f); mu.Unlock() }}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			// Identical content remains two distinct inputs, acknowledged once each.
			for i := 0; i < 2; i++ {
				input := codethreads.Input{ID: uuid.NewString(), Text: "hello", Delivery: "start_turn"}
				done := make(chan error, 1)
				go func() { done <- m.send(ctx, input, "native-thread") }()
				var request map[string]any
				select {
				case raw := <-writes:
					if json.Unmarshal(raw, &request) != nil {
						t.Fatal("bad input JSON")
					}
				case <-ctx.Done():
					t.Fatal("no input written")
				}
				observe := func(event any) { raw, _ := json.Marshal(event); m.observe(raw) }
				if provider == "claude" {
					if request["type"] != "user" || request["uuid"] != input.ID {
						t.Fatal("wrong Claude envelope", request)
					}
					observe(map[string]any{"type": "user", "uuid": "other", "message": map[string]any{"role": "user", "content": "hello"}})
				} else {
					params := request["params"].(map[string]any)
					if request["method"] != "turn/start" || params["clientUserMessageId"] != input.ID || params["threadId"] != "native-thread" {
						t.Fatal("wrong Codex envelope", request)
					}
					observe(map[string]any{"id": request["id"], "result": map[string]any{"turn": map[string]string{"id": "turn-test"}}})
					observe(map[string]any{"method": "item/started", "params": map[string]any{"turnId": "turn-test", "item": map[string]any{"type": "userMessage", "id": "native-item", "clientId": "other", "content": []any{map[string]string{"type": "text", "text": "hello"}}}}})
				}
				mu.Lock()
				n := len(frames)
				mu.Unlock()
				if n != i {
					t.Fatal("unrelated echo acknowledged")
				}
				var echo any
				if provider == "claude" {
					echo = request
				} else {
					echo = map[string]any{"method": "item/started", "params": map[string]any{"turnId": "turn-test", "item": map[string]any{"type": "userMessage", "id": "native-item", "clientId": input.ID, "content": []any{map[string]string{"type": "text", "text": "hello"}}}}}
				}
				observe(echo)
				observe(echo)
				mu.Lock()
				n = len(frames)
				f := frames[n-1]
				mu.Unlock()
				if n != i+1 || f.Type != "message.user" || f.InputID != input.ID || f.Text != "hello" {
					t.Fatal("echo lost or duplicated", f, n)
				}
				select {
				case <-done:
					t.Fatal("acknowledgement incorrectly ended turn")
				default:
				}
				if provider == "claude" {
					observe(map[string]string{"type": "result", "subtype": "success"})
				} else {
					observe(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]string{"id": "turn-test"}}})
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("turn did not finish")
				}
			}
		})
	}
}

func TestMessageFailureIsNotAcknowledged(t *testing.T) {
	for _, scenario := range []string{"rpc rejection", "missing echo", "process exit", "broken write"} {
		t.Run(scenario, func(t *testing.T) {
			writes := make(chan []byte, 1)
			processDone := make(chan struct{})
			m := &messages{provider: "codex", stdin: messageWriter{writes: writes, fail: scenario == "broken write"}, processDone: processDone, cancel: func() {}, emit: func(codethreads.Payload) { t.Error("unexpected acknowledgement") }}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- m.send(ctx, codethreads.Input{ID: uuid.NewString(), Text: "hello", Delivery: "start_turn"}, "native-thread")
			}()
			if scenario != "broken write" {
				var request map[string]any
				select {
				case raw := <-writes:
					json.Unmarshal(raw, &request)
				case <-ctx.Done():
					t.Fatal("no input written")
				}
				switch scenario {
				case "rpc rejection":
					raw, _ := json.Marshal(map[string]any{"id": request["id"], "error": map[string]string{"message": "rejected"}})
					m.observe(raw)
				case "missing echo":
					m.observe([]byte(`{"method":"turn/started","params":{"turn":{"id":"turn"}}}`))
					m.observe([]byte(`{"method":"turn/completed","params":{"turn":{"id":"turn"}}}`))
				case "process exit":
					close(processDone)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("missing failure")
				}
			case <-ctx.Done():
				t.Fatal("send hung")
			}
		})
	}
}

func TestMessageTextExcludesToolResults(t *testing.T) {
	for _, raw := range []string{`[{"type":"tool_result","text":"hello"}]`, `[]`, `null`, `[{"type":"text","text":"hello"},{"type":"image"}]`} {
		if _, ok := messageText(json.RawMessage(raw)); ok {
			t.Fatal("non-text content accepted", raw)
		}
	}
}
