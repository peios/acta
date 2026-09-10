package threadadapter

import (
	"acta2/internal/threads"
	"fmt"
	"path/filepath"
)

// File changes belong to an operation. Turn diffs are separate authoritative
// snapshots, never attributed to whichever operation happened most recently.
func (s *State) codexFileChanges(p threads.ProviderFrame, method string, params object, emit func(string, object)) bool {
	if str(params["threadId"]) == "" || str(params["threadId"]) != s.NativeID || str(params["turnId"]) == "" {
		return false
	}
	if method == "turn/diff/updated" {
		diff, ok := params["diff"].(string)
		if !ok {
			return false
		}
		emit("turn/diff", object{"turn_id": params["turnId"], "diff": diff})
		return true
	}
	item := obj(params["item"])
	if (method != "item/started" && method != "item/completed") || item["type"] != "fileChange" {
		return false
	}
	id := str(item["id"])
	status := map[string]string{"inProgress": "running", "completed": "completed", "failed": "failed", "declined": "declined"}[str(item["status"])]
	if id == "" || status == "" || (method == "item/started") != (status == "running") {
		return false
	}
	raw, ok := item["changes"].([]any)
	if !ok || len(raw) == 0 {
		return false
	}
	changes := make([]object, 0, len(raw))
	args := make([]object, 0, len(raw))
	for _, v := range raw {
		c := obj(v)
		kind := obj(c["kind"])
		action := str(kind["type"])
		diff, ok := c["diff"].(string)
		if str(c["path"]) == "" || !ok || (action != "add" && action != "update" && action != "delete") {
			return false
		}
		if kind["move_path"] != nil {
			if _, ok := kind["move_path"].(string); !ok {
				return false
			}
		}
		format := "unified"
		if action == "add" {
			format = "added_content"
		}
		if action == "delete" {
			format = "removed_content"
		}
		changes = append(changes, object{"path": c["path"], "kind": action, "move_path": nullable(kind["move_path"]), "diff": diff, "format": format})
		args = append(args, object{"path": c["path"], "kind": action, "move_path": nullable(kind["move_path"])})
	}
	t := s.tool(id)
	if t != nil && (t.Turn != str(params["turnId"]) || t.Name != "File changes") {
		return false
	}
	if t != nil && (terminalTool(t.Status) || method == "item/started") {
		return true
	}
	if t == nil {
		t = &ToolCall{ID: id, Turn: str(params["turnId"]), Name: "File changes", Category: "edit"}
		s.Tools[id] = t
	}
	t.Changes = changes
	t.Arguments = object{"changes": args}
	t.Label = fmt.Sprintf("Edit %d files", len(changes))
	if len(changes) == 1 {
		action := "Edit"
		if changes[0]["kind"] == "add" {
			action = "Write"
			t.Category = "write"
		}
		if changes[0]["kind"] == "delete" {
			action = "Delete"
		}
		t.Label = action + " " + filepath.Base(str(changes[0]["path"]))
	}
	t.Status = status
	if method == "item/started" {
		t.Started = stamp(params["startedAtMs"], true)
		if t.Started == nil {
			t.Started = p.ReceivedAt
		}
	} else {
		t.Completed = stamp(params["completedAtMs"], true)
		if t.Completed == nil {
			t.Completed = p.ReceivedAt
		}
	}
	emitTool(t, emit)
	return true
}
