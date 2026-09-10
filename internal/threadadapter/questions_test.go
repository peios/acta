package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestQuestionProviderMappingAndAnswers(t *testing.T) {
	claude, err := os.ReadFile("testdata/claude-question.json")
	if err != nil {
		t.Fatal(err)
	}
	codex := []byte(`{"id":42,"method":"item/tool/requestUserInput","params":{"threadId":"native","turnId":"turn","itemId":"tool","isBlocking":true,"questions":[{"id":"colour","header":"Colour","question":"Which sample colour?","isOther":true,"options":[{"label":"Blue","description":"Blue sample"},{"label":"Green","description":"Green sample"}]}]}}`)
	for provider, raw := range map[string][]byte{"claude": claude, "codex": codex} {
		t.Run(provider, func(t *testing.T) {
			const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
			a := ParseApproval(provider, id, "native", raw)
			if a == nil || len(a.Questions) != 1 {
				t.Fatal("question was not parsed")
			}
			if _, err := a.Reply(true); err == nil {
				t.Fatal("question allowed an approval without answers")
			}
			qid := a.Questions[0].ID
			for _, invalid := range []map[string][]string{nil, {"unknown": {"Blue"}}, {qid: {""}}, {qid: {"Blue", "Green"}}} {
				if _, err := a.Answer(invalid); err == nil {
					t.Fatal("invalid answer accepted", invalid)
				}
			}
			for _, value := range []string{"Blue", "Custom <colour> & ü\nnext line"} {
				reply, err := a.Answer(map[string][]string{qid: {value}})
				if err != nil {
					t.Fatal(err)
				}
				var m object
				_ = json.Unmarshal(reply, &m)
				if provider == "claude" {
					input := obj(obj(obj(m["response"])["response"])["updatedInput"])
					if obj(input["answers"])["Which sample colour?"] != value || len(list(input["questions"])) != 1 {
						t.Fatal(m)
					}
				} else if list(obj(obj(obj(m["result"])["answers"])[qid])["answers"])[0] != value {
					t.Fatal(m)
				}
			}
			s := State{RunID: id, NativeID: "native", Claude: ClaudeState{Turn: "turn"}}
			next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
			if err != nil || len(frames) != 2 || frames[1].Kind != "question/request" {
				t.Fatalf("%v %+v", err, frames)
			}
			checkpoint, _ := json.Marshal(next)
			next, err = DecodeState(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			cancel := `{"type":"control_cancel_request","request_id":"8690e4c3-df0f-4857-a6cc-b292ac87c27c"}`
			if provider == "codex" {
				cancel = `{"method":"serverRequest/resolved","params":{"threadId":"native","requestId":42}}`
			}
			_, frames, err = Map(next, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: 2, ReceivedAt: time.Now()}, "stdout", cancel)
			if err != nil || len(frames) != 2 || frames[1].Kind != "question/resolved" {
				t.Fatalf("%v %+v", err, frames)
			}
		})
	}
}

func TestQuestionMultipleAndUnsupportedShapes(t *testing.T) {
	m := object{"type": "control_request", "request_id": "native-request", "request": object{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "tool_use_id": "tool", "input": object{"questions": []any{object{"question": "Pick", "multiSelect": true, "options": []any{object{"label": "A", "description": "a"}, object{"label": "B", "description": "b"}}}}}}}
	a := parseApproval("claude", "run", "native", m)
	b, err := a.Answer(map[string][]string{"q1": {"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	var reply object
	_ = json.Unmarshal(b, &reply)
	if obj(obj(obj(obj(reply["response"])["response"])["updatedInput"])["answers"])["Pick"] != "A, B" {
		t.Fatal(reply)
	}
	q := obj(list(obj(obj(m["request"])["input"])["questions"])[0])
	q["isSecret"] = true
	if parseApproval("claude", "run", "native", m) != nil {
		t.Fatal("secret question became ordinary text")
	}
	delete(q, "isSecret")
	q["question"] = ""
	if parseApproval("claude", "run", "native", m) != nil {
		t.Fatal("invalid question became an approval")
	}
}
