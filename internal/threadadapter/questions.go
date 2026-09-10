package threadadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}
type Question struct {
	ID       string           `json:"id"`
	Header   string           `json:"header"`
	Text     string           `json:"text"`
	Multiple bool             `json:"multiple"`
	Options  []QuestionOption `json:"options"`
}

func isQuestion(provider string, m object) bool {
	return provider == "claude" && m["type"] == "control_request" && obj(m["request"])["tool_name"] == "AskUserQuestion" ||
		provider == "codex" && m["method"] == "item/tool/requestUserInput"
}

func parseQuestion(provider, run, native string, m object) *Approval {
	var id any
	var input object
	var turn, tool, method string
	if provider == "claude" {
		if session := str(m["session_id"]); session != "" && session != native {
			return nil
		}
		r := obj(m["request"])
		if r["subtype"] != "can_use_tool" {
			return nil
		}
		id, input, tool, method = m["request_id"], obj(r["input"]), str(r["tool_use_id"]), "AskUserQuestion"
		if str(id) == "" {
			return nil
		}
	} else {
		r := obj(m["params"])
		if native == "" || str(r["threadId"]) != native || str(r["turnId"]) == "" {
			return nil
		}
		id, input, turn, tool, method = m["id"], r, str(r["turnId"]), str(r["itemId"]), "item/tool/requestUserInput"
		switch id.(type) {
		case string:
			if str(id) == "" {
				return nil
			}
		case json.Number:
		default:
			return nil
		}
	}
	if tool == "" {
		return nil
	}
	questions := []Question{}
	seen := map[string]bool{}
	texts := map[string]bool{}
	for i, value := range list(input["questions"]) {
		q := obj(value)
		text := str(q["question"])
		qid := str(q["id"])
		if provider == "claude" {
			qid = fmt.Sprintf("q%d", i+1)
		}
		// Secret answers and visual previews need their own handling, rather than
		// silently being converted to ordinary persisted text.
		if qid == "" || seen[qid] || strings.TrimSpace(text) == "" || q["isSecret"] == true || (provider == "claude" && texts[text]) {
			return nil
		}
		seen[qid], texts[text] = true, true
		options := []QuestionOption{}
		labels := map[string]bool{}
		for _, value := range list(q["options"]) {
			o := obj(value)
			label := str(o["label"])
			if label == "" || labels[label] || o["preview"] != nil {
				return nil
			}
			labels[label] = true
			options = append(options, QuestionOption{Label: label, Description: str(o["description"])})
		}
		questions = append(questions, Question{ID: qid, Header: str(q["header"]), Text: text, Multiple: q["multiSelect"] == true, Options: options})
	}
	if len(questions) == 0 || len(questions) > 16 {
		return nil
	}
	encoded, _ := json.Marshal(id)
	a := &Approval{ID: approvalID(run, id), Native: encoded, Method: method, Input: input, Questions: questions}
	a.Display = object{"blocking": provider == "claude" || input["isBlocking"] != false, "question_id": a.ID, "questions": questions, "turn_id": nullable(turn), "tool_id": nullable(tool)}
	return a
}

// Answer validates against the request captured on this machine. The caller
// supplies only answers, never a replacement provider request or tool input.
func (a *Approval) Answer(answers map[string][]string) ([]byte, error) {
	if len(a.Questions) == 0 || len(answers) != len(a.Questions) {
		return nil, errors.New("Answer every question in this request.")
	}
	claude := object{}
	codex := object{}
	total := 0
	for _, q := range a.Questions {
		values := answers[q.ID]
		if len(values) == 0 || !q.Multiple && len(values) != 1 || len(values) > 64 {
			return nil, errors.New("Invalid number of answers.")
		}
		seen := map[string]bool{}
		for _, value := range values {
			total += len(value)
			if strings.TrimSpace(value) == "" || seen[value] || total > 64*1024 {
				return nil, errors.New("Enter non-empty answers of up to 64 KiB.")
			}
			seen[value] = true
		}
		claude[q.Text] = strings.Join(values, ", ")
		codex[q.ID] = object{"answers": values}
	}
	if a.Method == "AskUserQuestion" {
		input := maps.Clone(obj(a.Input))
		input["answers"] = claude
		return json.Marshal(object{"type": "control_response", "response": object{"subtype": "success", "request_id": a.Native, "response": object{"behavior": "allow", "updatedInput": input}}})
	}
	return json.Marshal(object{"id": a.Native, "result": object{"answers": codex}})
}
