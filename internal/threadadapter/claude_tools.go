package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"path/filepath"
	"strings"
)

func toolLabel(t *ToolCall) {
	t.Category, t.Label = "generic", t.Name
	if t.Name == "Read" || t.Name == "Write" || t.Name == "Edit" {
		t.Category = strings.ToLower(t.Name)
		t.Label = t.Name + " file"
		if path := str(t.Arguments["file_path"]); path != "" {
			t.Label = t.Name + " " + filepath.Base(path)
		}
	}
	if t.Name == "Bash" {
		t.Category = "command"
		t.Label = "Run command"
		if desc := str(t.Arguments["description"]); desc != "" {
			t.Label = desc
		}
	}
}
func (s *State) claudeTool(p threads.ProviderFrame, m object, emit func(string, object)) bool {
	if m["parent_tool_use_id"] != nil || (s.Claude.Turn == "" && m["type"] != "user") {
		return false
	}
	switch m["type"] {
	case "system":
		if m["subtype"] != "permission_denied" {
			return false
		}
		t := s.tool(str(m["tool_use_id"]))
		if t == nil || t.Turn != s.Claude.Turn || t.Name != str(m["tool_name"]) || str(m["message"]) == "" {
			return false
		}
		if terminalTool(t.Status) && t.Status != "failed" && t.Status != "permission_denied" {
			return false
		}
		t.Status = "permission_denied"
		t.PermissionDenial = object{"message": m["message"], "reason": nullable(str(m["decision_reason"])), "reason_type": nullable(str(m["decision_reason_type"]))}
		if t.Completed == nil {
			t.Completed = p.ReceivedAt
		}
		emitTool(t, emit)
		return true
	case "stream_event":
		e := obj(m["event"])
		index := strNumber(e["index"])
		if index == "" || s.Claude.Message == "" {
			return false
		}
		blockKey := s.Claude.Message + ":" + index
		if s.Claude.ToolBlocks == nil {
			s.Claude.ToolBlocks = map[string]string{}
		}
		if e["type"] == "content_block_start" {
			b := obj(e["content_block"])
			name, id := str(b["name"]), str(b["id"])
			if b["type"] != "tool_use" || name == "" || id == "" || obj(b["input"]) == nil {
				return false
			}
			if t := s.tool(id); t != nil {
				return t.Turn == s.Claude.Turn && t.Name == name
			}
			t := &ToolCall{ID: id, Turn: s.Claude.Turn, Name: name, Status: "preparing", Arguments: obj(b["input"])}
			toolLabel(t)
			s.Tools[id] = t
			s.Claude.ToolBlocks[blockKey] = id
			s.Claude.Blocks[index] = "tool_use"
			emitTool(t, emit)
			return true
		}
		t := s.tool(s.Claude.ToolBlocks[blockKey])
		if t == nil {
			return false
		}
		switch e["type"] {
		case "content_block_delta":
			d := obj(e["delta"])
			text, ok := d["partial_json"].(string)
			if d["type"] != "input_json_delta" || !ok {
				return false
			}
			if t.Status != "preparing" {
				return true
			}
			t.Partial += text
			emit("tool/arguments/delta", object{"tool_id": t.ID, "turn_id": t.Turn, "text": text})
			return true
		case "content_block_stop":
			if t.Status != "preparing" {
				return true
			}
			if t.Partial != "" {
				var args object
				if json.Unmarshal([]byte(t.Partial), &args) != nil || args == nil {
					return false
				}
				t.Arguments = args
			}
			t.Status = "pending"
			t.Partial = ""
			toolLabel(t)
			emitTool(t, emit)
			return true
		}
	case "assistant":
		message := obj(m["message"])
		parts := list(message["content"])
		// Validate the whole envelope first; mixed/unreviewed content stays unknown.
		if len(parts) == 0 {
			return false
		}
		for _, v := range parts {
			b := obj(v)
			name, id := str(b["name"]), str(b["id"])
			if b["type"] != "tool_use" || name == "" || id == "" || obj(b["input"]) == nil {
				return false
			}
			if t := s.tool(id); t != nil && (t.Turn != s.Claude.Turn || t.Name != name) {
				return false
			}
		}
		for _, v := range parts {
			b := obj(v)
			id := str(b["id"])
			t := s.tool(id)
			if t == nil {
				t = &ToolCall{ID: id, Turn: s.Claude.Turn, Name: str(b["name"])}
				s.Tools[id] = t
			}
			if terminalTool(t.Status) || t.BackgroundID != "" {
				continue
			}
			t.Arguments = obj(b["input"])
			t.Partial = ""
			t.Status = "pending"
			toolLabel(t)
			emitTool(t, emit)
		}
		return true
	case "user":
		parts := list(obj(m["message"])["content"])
		if len(parts) == 0 {
			return false
		}
		outputs := make([]string, len(parts))
		images := make([][]ToolImage, len(parts))
		attachments := make([][]ToolAttachment, len(parts))
		for i, v := range parts {
			b := obj(v)
			t := s.tool(str(b["tool_use_id"]))
			if b["type"] != "tool_result" || t == nil || (t.Turn != s.Claude.Turn && t.BackgroundID == "") {
				return false
			}
			if flag, exists := b["is_error"]; exists {
				if _, ok := flag.(bool); !ok {
					return false
				}
			}
			if text, ok := b["content"].(string); ok {
				outputs[i] = text
			} else {
				content, ok := b["content"].([]any)
				if !ok {
					return false
				}
				text := []string{}
				for _, v := range content {
					part := obj(v)
					if part["type"] == "document" {
						source := obj(part["source"])
						attachment, ok := toolPDF(str(source["media_type"]), str(source["data"]), str(t.Arguments["file_path"]))
						if source["type"] != "base64" || !ok {
							return false
						}
						attachments[i] = append(attachments[i], attachment)
						continue
					}
					if part["type"] == "image" {
						source := obj(part["source"])
						image, ok := toolImage(str(source["media_type"]), str(source["data"]))
						if source["type"] != "base64" || !ok {
							return false
						}
						images[i] = append(images[i], image)
						continue
					}
					if part["type"] == "tool_reference" && t.Name == "ToolSearch" {
						name := str(part["tool_name"])
						if name == "" {
							return false
						}
						text = append(text, "Available tool: "+name)
						continue
					}
					s, ok := part["text"].(string)
					if part["type"] != "text" || !ok {
						return false
					}
					text = append(text, s)
				}
				outputs[i] = strings.Join(text, "\n")
			}
		}
		for i, v := range parts {
			b := obj(v)
			t := s.tool(str(b["tool_use_id"]))
			// Denials can precede results. Keep the denial state while attaching the
			// exact result, and also allow structured result metadata to establish it.
			if t.ResultReceived || (terminalTool(t.Status) && t.Status != "permission_denied") {
				continue
			}
			t.ResultReceived = true
			if t.Status != "permission_denied" {
				t.Status = "completed"
				if b["is_error"] == true {
					t.Status = "failed"
				}
			}
			for _, meta := range list(m["tool_result_meta"]) {
				r := obj(meta)
				if r["id"] == t.ID && r["non_execution_kind"] == "user-rejected" {
					t.Status = "permission_denied"
					if t.PermissionDenial == nil {
						t.PermissionDenial = object{"message": outputs[i], "reason": nil, "reason_type": nil}
					}
				}
			}
			t.Output = &outputs[i]
			t.Images = images[i]
			t.Attachments = attachments[i]
			t.Completed = iso(m["timestamp"])
			if t.Completed == nil {
				t.Completed = p.ReceivedAt
			}
			if len(parts) == 1 && t.Name == "Bash" {
				result := obj(m["tool_use_result"])
				t.Stdout = textPointer(result["stdout"])
				t.Stderr = textPointer(result["stderr"])
				if id := str(result["backgroundTaskId"]); id != "" && b["is_error"] != true {
					if !s.startBackground(t, id, p) {
						return false
					}
					t.Status = "background"
					t.Completed = nil
				}
				if result["interrupted"] == true && t.Status != "permission_denied" {
					t.Status = "interrupted"
				}
			}
			// Child Bash results can omit tool_use_result. A prior lifecycle
			// promotion still means this is the launch receipt, not process exit.
			if t.BackgroundID != "" && t.Status == "completed" {
				t.Status = "background"
				t.Completed = nil
			}
			if len(parts) == 1 {
				s.claudeFileChanges(t, obj(m["tool_use_result"]), emit)
			} else {
				s.claudeFileChanges(t, nil, emit)
			}
			emitTool(t, emit)
		}
		return true
	}
	return false
}
