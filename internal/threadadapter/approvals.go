package threadadapter

import (
	"acta2/internal/threads"
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
)

// Approval retains native reply routing locally; only Display is sent as a frame.
// Its Acta UUID binds both a request and its one immutable decision to this run.
type Approval struct {
	Questions []Question
	ID        string
	Native    json.RawMessage
	Method    string
	Input     any
	Display   map[string]any
}

func approvalID(run string, id any) string {
	raw, _ := json.Marshal(id)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(run+"/approval/"+string(raw))).String()
}
func ParseApproval(provider, run, native string, raw []byte) *Approval {
	if !json.Valid(raw) {
		return nil
	}
	var m object
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&m) != nil {
		return nil
	}
	return parseApproval(provider, run, native, m)
}
func parseApproval(provider, run, native string, m object) *Approval {
	if isQuestion(provider, m) {
		return parseQuestion(provider, run, native, m)
	}
	var id any
	var title, reason, method, turn, tool string
	var details any
	if provider == "claude" {
		if str(m["type"]) != "control_request" {
			return nil
		}
		if session := str(m["session_id"]); session != "" && session != native {
			return nil
		}
		r := obj(m["request"])
		if str(r["subtype"]) != "can_use_tool" || str(r["tool_name"]) == "" || obj(r["input"]) == nil {
			return nil
		}
		id = m["request_id"]
		method = "can_use_tool"
		title = str(r["tool_name"])
		reason = str(r["decision_reason"])
		tool = str(r["tool_use_id"])
		details = object{"input": r["input"], "blocked_path": r["blocked_path"]}
		if str(id) == "" {
			return nil
		}
	} else if provider == "codex" {
		r := obj(m["params"])
		method = str(m["method"])
		if str(r["threadId"]) != native || native == "" {
			return nil
		}
		switch method {
		case "item/commandExecution/requestApproval":
			title = "Run command"
		case "item/fileChange/requestApproval":
			title = "Change files"
		case "item/permissions/requestApproval":
			title = "Grant access for this turn"
		default:
			return nil
		}
		id = m["id"]
		reason = str(r["reason"])
		turn = str(r["turnId"])
		tool = str(r["itemId"])
		details = r
		switch id.(type) {
		case string:
			if str(id) == "" {
				return nil
			}
		case json.Number:
		default:
			return nil
		}
	} else {
		return nil
	}
	nativeID, _ := json.Marshal(id)
	a := &Approval{ID: approvalID(run, id), Native: nativeID, Method: method}
	if provider == "claude" {
		a.Input = obj(m["request"])["input"]
	} else {
		a.Input = obj(m["params"])["permissions"]
	}
	a.Display = object{"approval_id": a.ID, "title": title, "reason": reason, "details": details, "turn_id": nullable(turn), "tool_id": nullable(tool)}
	return a
}
func (a *Approval) Reply(approve bool) ([]byte, error) {
	if len(a.Questions) > 0 {
		return nil, errors.New("This request requires an answer, not approval.")
	}
	var response any
	if a.Method == "can_use_tool" {
		body := object{"behavior": "deny", "message": "The user denied this request."}
		if approve {
			body = object{"behavior": "allow", "updatedInput": a.Input}
		}
		response = object{"type": "control_response", "response": object{"subtype": "success", "request_id": a.Native, "response": body}}
	} else {
		body := object{"decision": "decline"}
		if approve {
			body["decision"] = "accept"
		}
		if a.Method == "item/permissions/requestApproval" {
			granted := object{}
			if approve {
				for k, v := range obj(a.Input) {
					if v != nil {
						granted[k] = v
					}
				}
			}
			body = object{"permissions": granted, "scope": "turn"}
		}
		response = object{"id": a.Native, "result": body}
	}
	return json.Marshal(response)
}
func ApprovalResolved(provider, run, native string, raw []byte) string {
	if !json.Valid(raw) {
		return ""
	}
	var m object
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&m) != nil {
		return ""
	}
	return resolvedApproval(provider, run, native, m)
}
func resolvedApproval(provider, run, native string, m object) string {
	if provider == "claude" {
		if session := str(m["session_id"]); session != "" && session != native {
			return ""
		}
		if m["type"] == "control_cancel_request" && str(m["request_id"]) != "" {
			return approvalID(run, m["request_id"])
		}
		// The stdio permission bridge echoes its accepted response on stdout.
		// This resolves the original request; it is not another request or a
		// generic control acknowledgement (which has different semantics).
		if m["type"] == "control_response" {
			r := obj(m["response"])
			behavior := str(obj(r["response"])["behavior"])
			if r["subtype"] == "success" && str(r["request_id"]) != "" && (behavior == "allow" || behavior == "deny") {
				return approvalID(run, r["request_id"])
			}
		}
	}
	r := obj(m["params"])
	if provider == "codex" && m["method"] == "serverRequest/resolved" && str(r["threadId"]) == native && r["requestId"] != nil {
		return approvalID(run, r["requestId"])
	}
	return ""
}
func (s *State) approval(p threads.ProviderFrame, m object, emit func(string, object)) bool {
	if a := parseApproval(p.Provider, p.RunID, s.NativeID, m); a != nil {
		if p.Provider == "claude" {
			a.Display["turn_id"] = nullable(s.Claude.Turn)
		}
		if tool := s.tool(str(a.Display["tool_id"])); tool != nil {
			if a.Display["turn_id"] == nil {
				a.Display["turn_id"] = nullable(tool.Turn)
			}
			if details := obj(a.Display["details"]); details != nil && len(tool.Changes) > 0 {
				details["changes"] = tool.Changes
			}
		}
		if len(a.Questions) > 0 {
			if s.QuestionRequests == nil {
				s.QuestionRequests = map[string]bool{}
			}
			s.QuestionRequests[a.ID] = true
			emit("question/request", a.Display)
		} else {
			emit("approval/request", a.Display)
		}
		return true
	}
	if id := resolvedApproval(p.Provider, p.RunID, s.NativeID, m); id != "" {
		if s.QuestionRequests[id] {
			emit("question/resolved", object{"question_id": id})
		} else {
			emit("approval/resolved", object{"approval_id": id})
		}
		return true
	}
	return false
}
