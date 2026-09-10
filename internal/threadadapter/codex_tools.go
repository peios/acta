package threadadapter

import (
	"acta2/internal/threads"
	"path/filepath"
)

func (s *State) codexTool(p threads.ProviderFrame, method string, params object, emit func(string, object)) bool {
	item := obj(params["item"])
	lifecycle := (method == "item/started" || method == "item/completed") && item["type"] == "commandExecution"
	if !lifecycle && method != "item/commandExecution/outputDelta" {
		return false
	}
	if str(params["threadId"]) != s.NativeID || str(params["turnId"]) == "" {
		return false
	}
	id := str(item["id"])
	if !lifecycle {
		id = str(params["itemId"])
	}
	if id == "" {
		return false
	}
	t := s.tool(id)
	if !lifecycle {
		text, ok := params["delta"].(string)
		if !ok || t == nil || t.Turn != str(params["turnId"]) {
			return false
		}
		if terminalTool(t.Status) {
			return true
		}
		emit("tool/output/delta", object{"tool_id": id, "turn_id": t.Turn, "text": text})
		return true
	}
	command, ok := item["command"].(string)
	if !ok {
		return false
	}
	status := map[string]string{"inProgress": "running", "completed": "completed", "failed": "failed", "declined": "declined"}[str(item["status"])]
	if status == "" {
		return false
	}
	if method == "item/started" && status != "running" {
		return false
	}
	if method == "item/completed" && status == "running" {
		return false
	}
	if t != nil && (t.Turn != str(params["turnId"]) || t.Name != "Shell") {
		return false
	}
	if t != nil && (terminalTool(t.Status) || method == "item/started") {
		return true
	}
	if t == nil {
		t = &ToolCall{ID: id, Turn: str(params["turnId"]), Name: "Shell", Category: "command", Label: "Run command"}
		s.Tools[id] = t
	}
	t.Arguments = object{"command": command}
	t.CWD = str(item["cwd"])
	if process := str(item["processId"]); process != "" {
		t.ProcessID = process
	}
	t.Status = status
	// Only use the provider's explicit semantic annotation. Do not parse arbitrary
	// shell programs to guess whether they read or modify files.
	actions := list(item["commandActions"])
	if len(actions) == 1 && obj(actions[0])["type"] == "read" {
		a := obj(actions[0])
		name := str(a["name"])
		if name == "" {
			name = filepath.Base(str(a["path"]))
		}
		if name != "" && name != "." {
			t.Category = "read"
			t.Label = "Read " + name
		}
	}
	if method == "item/started" {
		t.Started = stamp(params["startedAtMs"], true)
		if t.Started == nil {
			t.Started = p.ReceivedAt
		}
	}
	if method == "item/completed" {
		t.Output = textPointer(item["aggregatedOutput"])
		t.ExitCode = number(item["exitCode"])
		t.Duration = number(item["durationMs"])
		if t.ExitCode != nil && numeric(t.ExitCode) != 0 && t.Status == "completed" {
			t.Status = "failed"
		}
		t.Completed = stamp(params["completedAtMs"], true)
		if t.Completed == nil {
			t.Completed = p.ReceivedAt
		}
	}
	emitTool(t, emit)
	if terminalTool(t.Status) {
		codexBackgroundNotice(t, emit)
	}
	return true
}
