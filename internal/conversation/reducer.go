package conversation

import (
	"acta2/internal/threads"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

func (r *Reducer) apply(f threads.Frame) error {
	c := r.Current
	d := Data(f)
	if c.RunID != f.RunID {
		*c = Current{RunID: f.RunID, Lanes: c.Lanes, Agents: c.Agents}
	}
	if c.Frames == nil {
		c.Frames = map[string]threads.Frame{}
	}
	if c.Background == nil {
		c.Background = map[string]threads.Frame{}
	}
	if f.Kind == "tool/call" && str(d["background_task_id"]) != "" {
		id := str(d["background_task_id"])
		if d["status"] == "background" {
			c.Background[id] = f
		} else if terminal(str(d["status"])) {
			delete(c.Background, id)
		}
	}
	if c.Pending == nil {
		c.Pending = map[string]threads.Frame{}
	}
	if c.MCP == nil {
		c.MCP = map[string]string{}
	}
	if c.TerminalMCP == nil {
		c.TerminalMCP = map[string]string{}
	}
	switch f.Kind {
	case "thread/configuration", "thread/status", "usage/context", "usage/account":
		name := f.Kind
		if f.Kind == "usage/account" {
			name += "/" + str(d["bucket_id"])
		}
		// Call-token updates often contain no context measurement. Keep the
		// entire last measured frame, including its original timestamp, for the
		// gauge. The incoming frame still updates turn usage/history below.
		if name != "usage/context" || hasContextMeasurement(d) || !hasContextMeasurement(Data(c.Frames[name])) {
			c.Frames[name] = f
		}
	}
	if strings.HasPrefix(f.Kind, "debug/") {
		visibility := "debug"
		if f.Kind == "debug/unknown" {
			visibility = "normal"
		}
		return r.event(f, visibility)
	}
	switch f.Kind {
	case "subagent/notification":
		return r.subagentNotification(f, d)
	case "subagent/status":
		return r.subagent(f, d)
	case "context/compaction":
		return r.compaction(f, d)
	case "message/user", "message/assistant", "message/assistant/delta":
		return r.message(f, d)
	case "tool/notification":
		return r.toolNotification(f, d)
	case "tool/call", "tool/arguments/delta", "tool/output/delta":
		return r.tool(f, d)
	case "thinking/started", "thinking/delta", "thinking/completed":
		return r.thinking(f, d)
	case "approval/review":
		return r.review(f, d)
	case "hook/started", "hook/completed":
		return r.hook(f, d)
	case "mcp/server/status":
		return r.mcp(f, d)
	case "approval/request", "question/request":
		requestID := str(d["approval_id"])
		if f.Kind == "question/request" {
			requestID = str(d["question_id"])
		}
		c.Pending[requestID] = f
		i, e := r.item(f, key("approval", f.RunID, requestID), "frame")
		if e != nil {
			return e
		}
		return r.save(i, f)
	case "approval/resolved", "question/resolved":
		requestID := str(d["approval_id"])
		if f.Kind == "question/resolved" {
			requestID = str(d["question_id"])
		}
		delete(c.Pending, requestID)
		// Resolution is persisted directly on the request item, even off screen.
		i, e := r.Store.Get(key("approval", f.RunID, requestID))
		if e != nil {
			return e
		}
		if i != nil {
			i.Payload["resolved"] = true
			if e = r.save(i, f); e != nil {
				return e
			}
		}
		return r.event(f, "hidden")
	case "turn/completed":
		if current, ok := c.Frames["context/compaction"]; ok && Data(current)["turn_id"] == d["turn_id"] && Data(current)["state"] == "in_progress" {
			delete(c.Frames, "context/compaction")
		}
		for id, pending := range c.Pending {
			if Data(pending)["turn_id"] == d["turn_id"] && !(pending.Kind == "question/request" && Data(pending)["blocking"] == false) {
				delete(c.Pending, id)
			}
		}
		if e := r.endItems(f, d); e != nil {
			return e
		}
		return r.turn(f, d)
	case "usage/context", "turn/diff", "turn/started":
		if e := r.turn(f, d); e != nil {
			return e
		}
	}
	visibility := "normal"
	switch f.Kind {
	case "thread/configuration", "thread/status", "usage/context", "usage/account", "turn/started", "turn/diff":
		visibility = "hidden"
	}
	return r.event(f, visibility)
}
func hasContextMeasurement(d Object) bool {
	context := obj(d["context"])
	used, usedOK := context["used_tokens"].(json.Number)
	capacity, capacityOK := context["capacity_tokens"].(json.Number)
	if !usedOK || !capacityOK {
		return false
	}
	u, usedErr := used.Float64()
	c, capacityErr := capacity.Float64()
	return usedErr == nil && capacityErr == nil && u >= 0 && c > 0
}

func (r *Reducer) message(f threads.Frame, d Object) error {
	assistant := f.Kind != "message/user"
	kind := "user-message"
	if assistant {
		kind = "assistant-message"
	}
	id := key("message", str(d["message_id"]))
	i, e := r.item(f, id, kind)
	if e != nil {
		return e
	}
	p := i.Payload
	if _, ok := p["text"]; !ok {
		p["text"] = ""
		p["completed"] = false
		p["phase"] = nil
	}
	if f.Kind == "message/assistant/delta" {
		if !yes(p["completed"]) {
			p["text"] = str(p["text"]) + str(d["text"])
		}
	} else if !yes(p["completed"]) || d["state"] == "completed" {
		if assistant {
			p["text"] = str(d["text"])
			p["phase"] = d["phase"]
		} else {
			parts := []string{}
			images := []any{}
			for _, v := range list(d["content"]) {
				part := obj(v)
				if part["type"] == "image" {
					images = append(images, part)
					continue
				}
				if part["type"] != "text" {
					return r.event(f, "normal")
				}
				parts = append(parts, str(part["text"]))
			}
			p["text"] = strings.Join(parts, "\n")
			p["images"] = images
			if submission := str(d["submission_id"]); submission != "" {
				p["id"] = "submission:" + submission
			}
		}
		p["completed"] = d["state"] == "completed"
		obj(p["frame"])["data"] = d
		if yes(p["completed"]) {
			p["completed_at"] = f.ReceivedAt
		}
	}
	i.Deleted = assistant && strings.TrimSpace(str(p["text"])) == ""
	return r.save(i, f)
}
func (r *Reducer) tool(f threads.Frame, d Object) error {
	i, e := r.item(f, key("tool", f.RunID, str(d["tool_id"])), "tool-call")
	if e != nil {
		return e
	}
	p := i.Payload
	if p["kind"] == "subagent" {
		return nil
	}
	if p["status"] == nil {
		p["status"] = "pending"
		p["toolId"] = d["tool_id"]
		p["turn"] = d["turn_id"]
		p["data"] = Object{}
		p["argumentsText"] = ""
		p["output"] = ""
		p["interruptedByTurn"] = false
	}
	status := str(p["status"])
	backgroundResume := yes(p["interruptedByTurn"]) && f.Kind == "tool/call" && d["status"] == "background" && str(d["background_task_id"]) != ""
	backgroundUpdate := f.Kind == "tool/call" && str(d["background_task_id"]) != "" && d["status"] == status
	if terminal(status) && !backgroundResume && !backgroundUpdate && !((yes(p["interruptedByTurn"]) || d["status"] == "permission_denied" && (status == "failed" || status == "permission_denied")) && f.Kind == "tool/call" && terminal(str(d["status"]))) {
		return nil
	}
	if f.Kind == "tool/call" {
		p["data"] = d
		p["status"] = d["status"]
		p["interruptedByTurn"] = false
		p["argumentsText"] = ""
		if d["arguments"] != nil {
			p["argumentsText"] = pretty(d["arguments"])
		}
		if output, ok := d["output"].(string); ok {
			p["output"] = output
		}
		if terminal(str(d["status"])) {
			p["completed_at"] = f.ReceivedAt
		}
	} else if f.Kind == "tool/output/delta" {
		p["output"] = str(p["output"]) + str(d["text"])
	} else if status == "preparing" || len(obj(p["data"])) == 0 {
		if p["argumentsText"] == "{}" {
			p["argumentsText"] = ""
		}
		p["argumentsText"] = str(p["argumentsText"]) + str(d["text"])
	}
	return r.save(i, f)
}
func (r *Reducer) thinking(f threads.Frame, d Object) error {
	i, e := r.item(f, key("thinking", f.RunID, str(d["thinking_id"])), "thinking")
	if e != nil {
		return e
	}
	p := i.Payload
	if p["text"] == nil {
		p["turn"] = d["turn_id"]
		p["text"] = ""
		p["tokens"] = nil
		p["started"] = nil
		p["ended"] = nil
		p["completed"] = false
		p["interrupted"] = false
	}
	complete := f.Kind == "thinking/completed"
	if (yes(p["completed"]) || yes(p["interrupted"])) && !complete {
		return nil
	}
	if f.Kind == "thinking/delta" {
		channel := str(d["channel"])
		parts := obj(i.Internal[channel])
		parts[strNumber(d["section_index"])] = str(parts[strNumber(d["section_index"])]) + str(d["text"])
		i.Internal[channel] = parts
		text := joinParts(obj(i.Internal["summary"]))
		if text == "" {
			text = joinParts(obj(i.Internal["content"]))
		}
		p["text"] = text
	} else {
		p["text"] = str(d["text"])
		if !complete {
			i.Internal["content"] = Object{"0": p["text"]}
		}
	}
	p["tokens"] = d["estimated_tokens"]
	if d["started_at"] != nil {
		p["started"] = d["started_at"]
	}
	if complete {
		p["completed"] = true
		p["interrupted"] = false
		p["ended"] = d["completed_at"]
		if p["ended"] == nil {
			p["ended"] = f.ReceivedAt
		}
		p["completed_at"] = p["ended"]
	}
	return r.save(i, f)
}
func strNumber(v any) string {
	switch n := v.(type) {
	case json.Number:
		return string(n)
	default:
		return "0"
	}
}
func joinParts(parts Object) string {
	keys := []int{}
	for k := range parts {
		n, _ := strconv.Atoi(k)
		keys = append(keys, n)
	}
	sort.Ints(keys)
	out := []string{}
	for _, k := range keys {
		out = append(out, str(parts[strconv.Itoa(k)]))
	}
	return strings.Join(out, "\n\n")
}
func (r *Reducer) review(f threads.Frame, d Object) error {
	i, e := r.item(f, key("review", f.RunID, str(d["review_id"])), "approval-review")
	if e != nil {
		return e
	}
	old := obj(i.Payload["data"])
	if old["status"] != nil && d["status"] == "in_progress" && (old["status"] != "in_progress" || yes(i.Payload["interrupted"])) {
		return nil
	}
	i.Payload["data"] = d
	i.Payload["interrupted"] = false
	if d["status"] != "in_progress" {
		i.Payload["completed_at"] = f.ReceivedAt
	}
	return r.save(i, f)
}
func (r *Reducer) hook(f threads.Frame, d Object) error {
	id := key("hook", f.RunID, str(d["hook_id"]))
	i, e := r.item(f, id, "hook")
	if e != nil {
		return e
	}
	if yes(i.Payload["completed"]) {
		return nil
	}
	if f.Kind == "hook/completed" {
		// Preserve the agreed hook-response position, moving only the start placeholder.
		i.Sequence = f.Sequence
		i.OutputIndex = f.OutputIndex
		i.Payload["frame"] = frameObject(f)
		i.Payload["completed_at"] = f.ReceivedAt
	}
	i.Payload["key"] = id
	i.Payload["name"] = d["name"]
	i.Payload["event"] = d["event"]
	i.Payload["completed"] = f.Kind == "hook/completed"
	return r.save(i, f)
}
func (r *Reducer) endItems(f threads.Frame, d Object) error {
	items, e := r.Store.InTurn(f.RunID, str(d["turn_id"]))
	if e != nil {
		return e
	}
	for _, i := range items {
		if i.LaneID != f.LaneID {
			continue
		}
		p := i.Payload
		changed := false
		switch p["kind"] {
		case "tool-call":
			if !terminal(str(p["status"])) && p["status"] != "background" {
				p["status"] = "interrupted"
				p["interruptedByTurn"] = true
				changed = true
			}
		case "thinking":
			if !yes(p["completed"]) {
				p["interrupted"] = true
				p["ended"] = d["completed_at"]
				if p["ended"] == nil {
					p["ended"] = f.ReceivedAt
				}
				changed = true
			}
		case "approval-review":
			if obj(p["data"])["status"] == "in_progress" {
				p["interrupted"] = true
				changed = true
			}
		case "frame":
			if kind := obj(p["frame"])["kind"]; kind == "approval/request" || kind == "question/request" && obj(obj(p["frame"])["data"])["blocking"] != false {
				p["resolved"] = true
				changed = true
			}
		}
		if changed {
			p["completed_at"] = f.ReceivedAt
			if e = r.save(i, f); e != nil {
				return e
			}
		}
	}
	return nil
}

// A notice has one identity, but its final history position is where the
// provider echoed it into context. Late context delivery moves, never duplicates.
func (r *Reducer) toolNotification(f threads.Frame, d Object) error {
	i, err := r.item(f, key("tool-notification", f.RunID, str(d["task_id"])), "frame")
	if err != nil {
		return err
	}
	prior := obj(obj(i.Payload["frame"])["data"])
	if i.Revision != 0 && yes(prior["context_entry"]) {
		return nil
	}
	i.Payload["frame"] = frameObject(f)
	if yes(d["context_entry"]) {
		i.Sequence = f.Sequence
		i.OutputIndex = f.OutputIndex
	}
	return r.save(i, f)
}

func (r *Reducer) compaction(f threads.Frame, d Object) error {
	i, e := r.item(f, key("compaction", f.RunID, str(d["compaction_id"])), "frame")
	if e != nil {
		return e
	}
	prior := obj(obj(i.Payload["frame"])["data"])
	if i.Revision != 0 && prior["state"] == "completed" && d["state"] != "completed" {
		return nil
	}
	if d["state"] == "completed" && (i.Revision == 0 || prior["state"] != "completed") {
		i.Sequence = f.Sequence
		i.OutputIndex = f.OutputIndex
		i.Payload["completed_at"] = f.ReceivedAt
	}
	i.Payload["frame"] = frameObject(f)
	i.Visibility = "hidden"
	if d["state"] == "completed" {
		i.Visibility = "normal"
	}
	current, exists := r.Current.Frames["context/compaction"]
	if !exists || Data(current)["compaction_id"] == d["compaction_id"] || d["state"] == "in_progress" || Data(current)["state"] != "in_progress" {
		r.Current.Frames["context/compaction"] = f
	}
	return r.save(i, f)
}
