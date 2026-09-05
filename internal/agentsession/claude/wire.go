package claude

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The wire side of Claude Code: how it is launched and what its stdin
// expects. Moved here from the harness so the harness holds no backend
// knowledge (ACT-37): Acta composes every line, the harness writes bytes.

// LaunchSpec is the claude command for a session.
type LaunchSpec struct {
	Cmd  string
	Args []string
	Env  map[string]string
}

// Launch composes the command line. A fresh session runs under Acta's id
// (--session-id) so the transcript on the host is findable; a resume
// continues the conversation the session's options name (after a /clear the
// process moves to a fresh transcript whose id is not the session's), else
// the session id itself.
func Launch(sessionID string, options map[string]any, resume bool) LaunchSpec {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--replay-user-messages",
		// Route permission prompts over the stream as control_request frames,
		// so Acta can show them as a modal and answer with a control_response.
		"--permission-prompt-tool", "stdio",
		// Stream text as it is written (stream_event frames) so the browser can
		// show a reply growing instead of waiting for the whole message.
		"--include-partial-messages",
		// Fast mode is refused in SDK/print mode unless the flag settings opt
		// in; with this it becomes a per-session /fast toggle.
		"--settings", `{"fastMode":true}`,
	}
	str := func(k string) string {
		if options == nil {
			return ""
		}
		s, _ := options[k].(string)
		return strings.TrimSpace(s)
	}
	if resume {
		if c := str("conversation"); c != "" {
			args = append(args, "--resume", c)
		} else {
			args = append(args, "--resume", sessionID)
		}
	} else {
		args = append(args, "--session-id", sessionID)
	}
	if v := str("permission_mode"); v != "" {
		args = append(args, "--permission-mode", v)
	}
	if v := str("model"); v != "" {
		args = append(args, "--model", v)
	}
	if v := str("effort"); v != "" {
		args = append(args, "--effort", v)
	}
	return LaunchSpec{Cmd: "claude", Args: args,
		// File checkpointing is off by default outside the TUI; with it on, a
		// rewind can restore the files a turn changed.
		Env: map[string]string{"CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING": "1"}}
}

// Kind is a stream-json line's "type", the transcript's coarse label.
func Kind(line json.RawMessage) string {
	var m struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &m) == nil && m.Type != "" {
		return m.Type
	}
	return "event"
}

// Stored: everything but the streamed deltas, which the assistant frame that
// follows supersedes, and a background task's partial output chunks.
func Stored(kind string, line json.RawMessage) bool {
	switch kind {
	case "stream_event":
		return false
	case "task_output":
		var m struct {
			Done bool `json:"done"`
		}
		return json.Unmarshal(line, &m) == nil && m.Done
	}
	return true
}

// Image is a picture attached to a message, base64 with its media type.
type Image struct {
	MediaType string
	Data      string
}

// InputLine is the stream-json user message: plain text when there are no
// pictures (what every consumer of the transcript expects), else the block
// array with the images first and the text after.
func InputLine(text string, images []Image) []byte {
	var content any = text
	if len(images) > 0 {
		blocks := make([]map[string]any, 0, len(images)+1)
		for _, im := range images {
			blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": im.MediaType, "data": im.Data}})
		}
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		content = blocks
	}
	b, _ := json.Marshal(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": content},
		"parent_tool_use_id": nil,
	})
	return b
}

// InterruptLine ends the current turn but keeps the process (and its warm
// context) alive, so the next message needs no resume.
func InterruptLine() []byte {
	return []byte(fmt.Sprintf(`{"type":"control_request","request_id":"interrupt-%d","request":{"subtype":"interrupt"}}`, time.Now().UnixMilli()))
}

// Op mirrors agentsession.BrowserOp (the package stays free of hub types).
type Op struct {
	Op, ID, Outcome, Message, Key, Value, Target, Question string
	Input, Permissions, Answers, Content                   json.RawMessage
	DryRun                                                 bool
}

// ControlLines turns a browser operation into Claude Code's control protocol.
func ControlLines(op Op) [][]byte {
	line := func(v any) []byte { b, _ := json.Marshal(v); return b }
	req := func(id string, request map[string]any) []byte {
		return line(map[string]any{"type": "control_request", "request_id": id, "request": request})
	}
	switch op.Op {
	case "answer":
		var resp map[string]any
		switch op.Outcome {
		case "accept", "decline", "cancel":
			resp = map[string]any{"action": op.Outcome}
			if op.Outcome == "accept" && len(op.Content) > 0 {
				resp["content"] = op.Content
			}
		case "deny":
			resp = map[string]any{"behavior": "deny", "message": op.Message}
			if resp["message"] == "" {
				resp["message"] = "Denied by the user in Acta"
			}
		default:
			input := json.RawMessage(op.Input)
			if len(op.Answers) > 0 {
				var in map[string]any
				_ = json.Unmarshal(op.Input, &in)
				if in == nil {
					in = map[string]any{}
				}
				in["answers"] = op.Answers
				input, _ = json.Marshal(in)
			}
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			resp = map[string]any{"behavior": "allow", "updatedInput": input}
			if len(op.Permissions) > 0 {
				resp["updatedPermissions"] = op.Permissions
			}
		}
		return [][]byte{line(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": op.ID, "response": resp}})}
	case "setting":
		switch op.Key {
		case "permission_mode":
			return [][]byte{req(op.ID, map[string]any{"subtype": "set_permission_mode", "mode": op.Value})}
		case "model":
			return [][]byte{req(op.ID, map[string]any{"subtype": "set_model", "model": op.Value})}
		case "output_style":
			return [][]byte{req(op.ID, map[string]any{"subtype": "update_settings", "source": "localSettings", "settings": map[string]any{"outputStyle": op.Value}})}
		case "effort":
			return [][]byte{InputLine("/effort "+op.Value, nil)}
		case "fast":
			return [][]byte{InputLine("/fast", nil)}
		}
	case "catalog":
		return [][]byte{
			req("models-"+op.ID, map[string]any{"subtype": "list_models"}),
			req("settings-"+op.ID, map[string]any{"subtype": "get_settings"}),
			req("init-"+op.ID, map[string]any{"subtype": "initialize"}),
		}
	case "rewind":
		return [][]byte{req(op.ID, map[string]any{"subtype": "rewind_conversation", "target_message_uuid": op.Target})}
	case "rewind_files":
		return [][]byte{req(op.ID, map[string]any{"subtype": "rewind_files", "user_message_id": op.Target, "dry_run": op.DryRun})}
	case "side_question":
		return [][]byte{req(op.ID, map[string]any{"subtype": "side_question", "question": op.Question})}
	}
	return nil
}

// Conversation: the session id an init frame reports, when it is not the one
// the session was spawned under (a /clear moved the process to a fresh
// transcript), so a resume can name it.
func Conversation(sessionID, kind string, line json.RawMessage) string {
	if kind != "system" {
		return ""
	}
	var m struct {
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &m) != nil || m.Subtype != "init" || m.SessionID == "" || m.SessionID == sessionID {
		return ""
	}
	return m.SessionID
}

// Option: a set_permission_mode or set_model control, or an "/effort <level>"
// message, is a per-session choice a later resume should start with.
func Option(kind string, line json.RawMessage) (key, value string, ok bool) {
	switch kind {
	case "control":
		var op struct {
			Op, Key, Value string
		}
		if json.Unmarshal(line, &op) == nil && op.Op == "setting" {
			switch op.Key {
			case "permission_mode", "effort":
				return op.Key, op.Value, true
			case "model":
				if op.Value == "default" {
					return "model", "", true
				}
				return "model", op.Value, true
			}
			return "", "", false
		}
		var probe struct {
			Type    string `json:"type"`
			Request struct {
				Subtype string `json:"subtype"`
				Mode    string `json:"mode"`
				Model   string `json:"model"`
			} `json:"request"`
		}
		if json.Unmarshal(line, &probe) != nil || probe.Type != "control_request" {
			return "", "", false
		}
		switch {
		case probe.Request.Subtype == "set_permission_mode" && probe.Request.Mode != "":
			return "permission_mode", probe.Request.Mode, true
		case probe.Request.Subtype == "set_model" && probe.Request.Model != "":
			if probe.Request.Model == "default" {
				return "model", "", true
			}
			return "model", probe.Request.Model, true
		}
	case "input":
		var m struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(line, &m) != nil {
			return "", "", false
		}
		f := strings.Fields(strings.TrimSpace(m.Text))
		if len(f) == 2 && f[0] == "/effort" {
			switch f[1] {
			case "low", "medium", "high", "xhigh", "max":
				return "effort", f[1], true
			}
		}
	}
	return "", "", false
}

var bgTaskRe = regexp.MustCompile(`Command running in background with ID: (\S+)\. Output is being written to: (\S+?)\.?(?:\s|$)`)

// BackgroundTask: a Bash call run in the background answers at once with the
// task id and the file its output is written to.
func BackgroundTask(kind string, line json.RawMessage) (id, path string, ok bool) {
	if kind != "user" || !strings.Contains(string(line), "Command running in background with ID") {
		return "", "", false
	}
	m := bgTaskRe.FindSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return string(m[1]), string(m[2]), true
}

// TaskEnded: the task_notification (or a task_updated leaving "running")
// that closes a background task.
func TaskEnded(kind string, line json.RawMessage) (string, bool) {
	if kind != "system" {
		return "", false
	}
	var m struct {
		Subtype string `json:"subtype"`
		TaskID  string `json:"task_id"`
		Patch   struct {
			Status string `json:"status"`
		} `json:"patch"`
	}
	if json.Unmarshal(line, &m) != nil || m.TaskID == "" {
		return "", false
	}
	ended := m.Subtype == "task_notification" || (m.Subtype == "task_updated" && m.Patch.Status != "" && m.Patch.Status != "running")
	return m.TaskID, ended
}

// ResumeFailed: Claude Code writes a session's conversation only once it has
// taken a turn, so resuming one that was spawned but never used fails
// outright with this message.
func ResumeFailed(code int, stderr string) bool {
	return code != 0 && strings.Contains(stderr, "No conversation found")
}

// RenameLine retitles Claude Code's own transcript (its resume picker, window
// title). The name peers see (ListAgents, SendMessage) is set by the
// "/rename" command, which goes in as an input line.
func RenameLine(title string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "acta-rename-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		"request":    map[string]any{"subtype": "rename_session", "title": title},
	})
	return b
}

// TitleRequestLine asks Claude Code for a short title describing the
// session's first message, persisted on its side too.
func TitleRequestLine(requestID, description string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    map[string]any{"subtype": "generate_session_title", "description": description, "persist": true},
	})
	return b
}

// TitleAnswer recognises the control_response to a rename or title request:
// the request id and, for a title request, the title it chose.
func TitleAnswer(kind string, line json.RawMessage) (requestID, title string, ok bool) {
	if kind != "control_response" {
		return "", "", false
	}
	var m struct {
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				Title string `json:"title"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &m) != nil {
		return "", "", false
	}
	rid := m.Response.RequestID
	if !strings.HasPrefix(rid, "acta-title-") && !strings.HasPrefix(rid, "acta-rename-") {
		return "", "", false
	}
	return rid, strings.TrimSpace(m.Response.Response.Title), true
}
