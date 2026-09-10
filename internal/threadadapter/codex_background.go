package threadadapter

import (
	"fmt"
	"sort"
)

// Codex can finish a turn while a unified-exec process remains alive. Only
// process-backed commands explicitly still running qualify; an unfinished tool
// without this evidence keeps the usual interrupted-at-turn-end behaviour.
func (s *State) codexBackground(turn string, emit func(string, object)) {
	ids := []string{}
	for id, t := range s.Tools {
		if t.Turn == turn && t.Name == "Shell" && t.Status == "running" && t.ProcessID != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids) // Stable output indices when replaying the provider frame.
	for _, id := range ids {
		t := s.Tools[id]
		t.BackgroundID = "command/" + id
		t.Status = "background"
		emitTool(t, emit)
	}
}

func codexBackgroundNotice(t *ToolCall, emit func(string, object)) {
	if t.BackgroundID == "" || t.BackgroundNotified {
		return
	}
	t.BackgroundNotified = true
	status := t.Status
	if status != "completed" && status != "interrupted" {
		status = "failed"
	}
	summary := "Command " + status + "."
	if t.ExitCode != nil {
		summary = fmt.Sprintf("Command %s with exit code %v.", status, t.ExitCode)
	}
	emit("tool/notification", object{"task_id": t.BackgroundID, "tool_id": t.ID, "turn_id": t.Turn, "label": t.Label, "status": status, "summary": summary, "context_entry": false})
}
