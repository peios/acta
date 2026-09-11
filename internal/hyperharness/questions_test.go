package hyperharness

import (
	"acta/internal/harnesspipe"
	"acta/internal/threadadapter"
	"acta/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestQuestionAnswerDurableAndRunBound(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			run := uuid.NewString()
			raw := json.RawMessage(`{"id":7,"method":"item/tool/requestUserInput","params":{"threadId":"native","turnId":"turn","itemId":"tool","isBlocking":true,"questions":[{"id":"colour","question":"Colour?","header":"Colour","options":null}]}}`)
			qid := "colour"
			if provider == "claude" {
				raw = json.RawMessage(`{"type":"control_request","request_id":"request","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"tool","input":{"questions":[{"question":"Colour?","header":"Colour","options":[{"label":"Blue","description":"Blue"}],"multiSelect":false}]}}}`)
				qid = "q1"
			}
			pipe := &approvalPipe{framePipe: framePipe{frames: []harnesspipe.RawFrame{{RunID: run, Sequence: 1, Stream: "stdout", Data: raw}}}, writes: map[string]string{}}
			dir := filepath.Join(t.TempDir(), "controller")
			c, err := NewController(t.Context(), dir, pipe)
			if err != nil {
				t.Fatal(err)
			}
			a := threadadapter.ParseApproval(provider, run, "native", raw)
			c.records[run] = &localThread{Thread: threads.Descriptor{ID: run, RunID: run, Provider: provider, ProviderID: "native", State: "running"}, Commands: map[string]threads.Result{}, Requests: map[string]threads.Control{}}
			q := threads.Control{ID: a.ID, QuestionID: a.ID, ThreadID: run, RunID: run, Action: "answer", Answers: map[string][]string{qid: {"Blue"}}}
			if err = c.Control(q); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			var result threads.Result
			for time.Now().Before(deadline) {
				c.mu.Lock()
				result = c.records[run].Commands[q.ID]
				c.mu.Unlock()
				if result.Outcome != "" {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if result.Outcome != "accepted" || result.Decision != "answer" || !reflect.DeepEqual(result.Answers, q.Answers) {
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
			q.Answers[qid][0] = "Green"
			if c.Control(q) == nil {
				t.Fatal("answer was mutable")
			}
			q.ID = uuid.NewString()
			q.QuestionID = q.ID
			q.RunID = uuid.NewString()
			if c.Control(q) == nil {
				t.Fatal("stale run accepted")
			}
			pipe.mu.Lock()
			defer pipe.mu.Unlock()
			if len(pipe.writes) != 1 {
				t.Fatal("provider answer repeated", pipe.writes)
			}
		})
	}
}

func TestQuestionExpiredBeforeAnswer(t *testing.T) {
	raw := json.RawMessage(`{"id":7,"method":"item/tool/requestUserInput","params":{"threadId":"native","turnId":"turn","itemId":"tool","isBlocking":true,"questions":[{"id":"colour","question":"Colour?"}]}}`)
	for _, end := range []string{`{"method":"serverRequest/resolved","params":{"threadId":"native","requestId":7}}`, `{"method":"turn/completed","params":{"threadId":"native","turn":{"id":"turn"}}}`} {
		pipe := &approvalPipe{framePipe: framePipe{frames: []harnesspipe.RawFrame{{RunID: "run", Sequence: 1, Stream: "stdout", Data: raw}, {RunID: "run", Sequence: 2, Stream: "stdout", Data: json.RawMessage(end)}}}}
		c := Controller{pipe: pipe}
		a := threadadapter.ParseApproval("codex", "run", "native", raw)
		_, err := c.pendingApproval(t.Context(), &localThread{Thread: threads.Descriptor{RunID: "run", Provider: "codex", ProviderID: "native"}}, a.ID)
		if err == nil {
			t.Fatal("expired question remained pending")
		}
	}
}
