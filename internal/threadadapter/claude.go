package threadadapter

import (
	"acta/internal/threads"
	"reflect"
	"sort"
	"strings"
)

type ClaudeMessage struct {
	StreamedText bool
	Turn         string
	Text         string
	Started      any
	Complete     bool
	Usage        object
}
type ClaudeState struct {
	Compaction           string
	CompactionBoundaries map[string]string
	FileDiffs            map[string]*ClaudeTurnDiff
	Cancelled            map[string]bool
	ToolBlocks           map[string]string
	Account              object
	Turn                 string
	Message              string
	Messages             map[string]*ClaudeMessage
	Blocks               map[string]string
	Fast                 string
	MCP                  map[string]string
}

func (s *State) claudeConfig(patch object, emit func(string, object)) {
	next := object{}
	for k, v := range s.Configuration {
		next[k] = v
	}
	for k, v := range patch {
		next[k] = v
	}
	if !reflect.DeepEqual(next, s.Configuration) {
		s.Configuration = next
		emit("thread/configuration", next)
	}
}
func claudePermissions(mode string) object {
	normalized := map[string]string{"default": "ask", "acceptEdits": "accept_edits", "bypassPermissions": "bypass", "dontAsk": "dont_ask", "auto": "automatic", "plan": "ask"}[mode]
	if normalized == "" {
		normalized = "unknown"
	}
	// Permission prompts do not establish an OS sandbox boundary.
	return object{"mode": normalized, "filesystem": "unknown", "network": "unknown"}
}
func (s *State) claudeStatus(state string, emit func(string, object)) bool {
	status := "active"
	waiting := []string{}
	switch state {
	case "idle":
		status = "idle"
	case "running", "queued", "started":
	case "requires_action":
		// Claude does not distinguish approval from other required action here.
	default:
		return false
	}
	emit("thread/status", object{"status": status, "waiting_for": waiting})
	return true
}
func (s *State) claudeMCP(servers []any, emit func(string, object)) bool {
	if s.Claude.MCP == nil {
		s.Claude.MCP = map[string]string{}
	}
	known := true
	for _, v := range servers {
		m := obj(v)
		name := str(m["name"])
		native := str(m["status"])
		status := map[string]string{"pending": "starting", "connected": "ready", "failed": "failed", "needs-auth": "failed", "disabled": "cancelled"}[native]
		if name == "" || status == "" {
			known = false
			continue
		}
		if s.Claude.MCP[name] == native {
			continue
		}
		s.Claude.MCP[name] = native
		var reason any
		if native == "needs-auth" {
			reason = "Authentication required"
		}
		if native == "disabled" {
			reason = "Disabled in provider configuration"
		}
		emit("mcp/server/status", object{"server_name": name, "status": status, "error": providerError(m["error"]), "failure_reason": reason})
	}
	return known
}
func (s *State) convertClaude(p threads.ProviderFrame, m object, emit func(string, object)) (string, string) {
	if s.approval(p, m, emit) {
		return "debug/local", "Handled permission request lifecycle."
	}
	local := func() (string, string) { return "debug/local", "Handled Claude control or lifecycle metadata." }
	unknown := func() (string, string) {
		return "debug/unknown", "Claude frame contains an unreviewed type, identity or shape."
	}
	if s.Claude.Messages == nil {
		s.Claude.Messages = map[string]*ClaudeMessage{}
	}
	if s.Claude.Blocks == nil {
		s.Claude.Blocks = map[string]string{}
	}
	if native := str(m["session_id"]); native != "" && native != s.NativeID {
		return unknown()
	}
	typ := str(m["type"])
	if typ == "system" && m["subtype"] == "vcs_state_changed" {
		if m["kind"] != "push" || str(m["branch"]) == "" || str(m["cwd"]) == "" {
			return unknown()
		}
		emit("vcs/push", object{"branch": m["branch"], "cwd": m["cwd"], "turn_id": nullable(s.Claude.Turn)})
		return local()
	}
	if typ == "control_response" {
		r := obj(m["response"])
		id := str(r["request_id"])
		if !strings.HasPrefix(id, p.RunID+"/") {
			return unknown()
		}
		call := strings.Split(strings.TrimPrefix(id, p.RunID+"/"), "/")[0]
		switch call {
		case "rename_session", "stop_task", "interrupt", "initialize", "get_settings", "get_binary_version", "list_models", "apply_flag_settings", "set_permission_mode", "mcp_status", "get_usage", "get_context_usage":
		default:
			return unknown()
		}
		if r["subtype"] == "error" {
			return local()
		}
		if r["subtype"] != "success" {
			return unknown()
		}
		r = obj(r["response"])
		switch call {
		case "rename_session":
			return local()
		case "initialize":
			patch := object{}
			if fast := str(r["fast_mode_state"]); fast == "off" || fast == "on" || fast == "cooldown" {
				s.Claude.Fast = fast
				patch["fast_mode"] = fast == "on"
			}
			if mode := str(r["current_permission_mode"]); mode != "" {
				patch["permissions"] = claudePermissions(mode)
				if mode == "plan" {
					patch["work_mode"] = "plan"
				} else {
					patch["work_mode"] = "execute"
				}
			}
			if len(patch) > 0 {
				s.claudeConfig(patch, emit)
			}
			if !s.claudeStatus(str(r["session_state"]), emit) {
				return unknown()
			}
		case "get_settings":
			applied := obj(r["applied"])
			if str(applied["model"]) == "" {
				return unknown()
			}
			s.claudeConfig(object{"model": applied["model"], "effort": nullable(applied["effort"])}, emit)
		case "mcp_status":
			if !s.claudeMCP(list(r["mcpServers"]), emit) {
				return unknown()
			}
		case "get_context_usage":
			if number(r["totalTokens"]) == nil || number(r["maxTokens"]) == nil {
				return unknown()
			}
			emit("usage/context", object{"turn_id": nullable(s.Claude.Turn), "context": object{"used_tokens": number(r["totalTokens"]), "capacity_tokens": number(r["maxTokens"]), "estimated": true}, "cumulative": nil, "last_request": claudeTokens(obj(r["apiUsage"]))})
		case "get_usage":
			s.claudeAccount(r, emit)
		}
		return local()
	}
	if typ == "control_request" {
		return unknown()
	} // Approval/tool requests are deliberately not auto-approved.
	if str(m["session_id"]) == "" {
		return unknown()
	}
	if s.claudeCompaction(p, m, emit) {
		return local()
	}
	if s.claudeBackground(p, m, emit) {
		if m["subtype"] == "background_tasks_changed" {
			return "debug/dropped", "Task inventory adds no tool lifecycle information."
		}
		return local()
	}
	if s.claudeTool(p, m, emit) {
		return local()
	}
	if s.claudeThinking(p, m, emit) {
		return local()
	}
	switch typ {
	case "command_lifecycle":
		id := str(m["command_uuid"])
		if id == "" {
			return unknown()
		}
		switch str(m["state"]) {
		case "queued":
			// Acceptance does not move the active turn: existing output can still
			// arrive before this command starts. Settle only the pending message.
			s.claudeSubmission(p, id, emit)
		case "started":
			s.claudeSubmission(p, id, emit)
			s.Claude.Turn = id
			s.claudeStatus("running", emit)
			if s.Turns[id] == "" {
				s.Turns[id] = "started"
				emit("turn/started", object{"turn_id": id, "started_at": nil})
			}
		case "cancelled":
			// Goal interruption can return a successful result before this
			// authoritative lifecycle cancellation. Correct that same turn.
			if s.Turns[id] == "" && s.Submissions[id] != p.RunID {
				return unknown()
			}
			if s.Claude.Cancelled[id] {
				return "debug/dropped", "Turn cancellation was already recorded."
			}
			if s.Claude.Cancelled == nil {
				s.Claude.Cancelled = map[string]bool{}
			}
			s.Claude.Cancelled[id] = true
			s.Claude.Compaction = ""
			emit("turn/completed", object{"turn_id": id, "outcome": "interrupted", "error": nil, "started_at": nil, "completed_at": nil, "duration_ms": nil})
		case "completed": // Result, not queue lifecycle, contains the terminal outcome.
		default:
			return unknown()
		}
		return local()
	case "session_state_changed":
		if !s.claudeStatus(str(m["state"]), emit) {
			return unknown()
		}
		return local()
	case "system":
		switch str(m["subtype"]) {
		case "hook_started", "hook_response":
			if claudeHook(m, emit) {
				return local()
			}
			return unknown()
		case "background_tasks_changed":
			if tasks, ok := m["tasks"].([]any); ok && len(tasks) == 0 {
				return "debug/dropped", "No background tasks to represent."
			}
			return unknown()
		case "session_state_changed":
			if !s.claudeStatus(str(m["state"]), emit) {
				return unknown()
			}
			return local()
		case "init":
			patch := object{"cwd": nullable(m["cwd"]), "model": nullable(m["model"])}
			if mode := str(m["permissionMode"]); mode != "" {
				patch["permissions"] = claudePermissions(mode)
				patch["work_mode"] = "execute"
				if mode == "plan" {
					patch["work_mode"] = "plan"
				}
			}
			if fast := str(m["fast_mode_state"]); fast == "on" || fast == "off" || fast == "cooldown" {
				patch["fast_mode"] = fast == "on"
			}
			s.claudeConfig(patch, emit)
			s.claudeMCP(list(m["mcp_servers"]), emit)
			return local()
		case "status":
			if m["status"] == nil {
				return local()
			}
			if m["status"] == "requesting" {
				s.claudeStatus("running", emit)
				return local()
			}
			return unknown()
		default:
			return unknown()
		}
	case "user":
		id := str(m["uuid"])
		message := obj(m["message"])
		markerParts := list(message["content"])
		marker := ""
		if len(markerParts) == 1 {
			marker = str(obj(markerParts[0])["text"])
		}
		if id != "" && m["isReplay"] != true && m["parent_tool_use_id"] == nil &&
			s.Submissions[id] == "" && message["role"] == "user" && len(markerParts) == 1 &&
			obj(markerParts[0])["type"] == "text" &&
			(marker == "[Request interrupted by user]" || marker == "[Request interrupted by user for tool use]") {
			return "debug/dropped", "Provider interruption marker; turn completion supplies the conversation summary."
		}
		if id == "" || m["isReplay"] != true || m["parent_tool_use_id"] != nil {
			return unknown()
		}
		if s.Messages[id].Completed {
			return "debug/dropped", "User message was already recorded."
		}
		content := obj(m["message"])["content"]
		if s.Submissions[id] != p.RunID && s.Turns[id] == "" && id != p.ThreadID {
			if text, ok := content.(string); ok && strings.HasPrefix(text, "<local-command-stdout>") && strings.HasSuffix(text, "</local-command-stdout>") {
				return "debug/local", "Provider local-command receipt; effective settings are read back separately."
			}
			return unknown()
		}
		parts := []any{}
		if text, ok := content.(string); ok {
			parts = append(parts, object{"type": "text", "text": claudeCommandText(text)})
		} else {
			for _, v := range list(content) {
				part := obj(v)
				if part["type"] == "image" {
					image, valid := inputImage(part, "claude")
					if !valid {
						return unknown()
					}
					parts = append(parts, image)
					continue
				}
				if part["type"] != "text" {
					return unknown()
				}
				parts = append(parts, object{"type": "text", "text": str(part["text"])})
			}
		}
		if len(parts) == 0 {
			return unknown()
		}
		s.Claude.Turn = id
		data := object{"message_id": id, "turn_id": id, "state": "completed", "started_at": nil, "completed_at": iso(m["timestamp"]), "content": parts}
		if s.Submissions[id] == p.RunID {
			data["submission_id"] = id
		}
		emit("message/user", data)
		s.Messages[id] = Message{Turn: id, Kind: "userMessage", Completed: true}
		return local()
	case "stream_event":
		if m["parent_tool_use_id"] != nil {
			return unknown()
		}
		turn := str(m["user_message_uuid"])
		if turn == "" {
			turn = s.Claude.Turn
		}
		if turn == "" {
			return unknown()
		}
		event := obj(m["event"])
		kind := str(event["type"])
		switch kind {
		case "message_start":
			message := obj(event["message"])
			id := str(message["id"])
			if id == "" {
				return unknown()
			}
			// Notifications can queue while an earlier reply is running, or
			// coalesce into one reply. The provider's next new message after a
			// completed turn is the reliable boundary; do not count notices.
			if str(m["user_message_uuid"]) == "" && s.Turns[turn] == "completed" {
				if prior := s.Claude.Messages[id]; prior != nil {
					turn = prior.Turn
				} else {
					turn = "background/" + id
					s.Turns[turn] = "started"
					emit("turn/started", object{"turn_id": turn, "started_at": p.ReceivedAt})
				}
			}
			s.Claude.Message = id
			s.Claude.Turn = turn
			s.Claude.Blocks = map[string]string{}
			if s.Claude.Messages[id] == nil {
				s.Claude.Messages[id] = &ClaudeMessage{Turn: turn, Usage: obj(message["usage"])}
			}
			return local()
		case "content_block_start":
			block := obj(event["content_block"])
			index := strNumber(event["index"])
			if index == "" {
				return unknown()
			}
			s.Claude.Blocks[index] = str(block["type"])
			if block["type"] != "text" {
				return unknown()
			}
			msg := s.Claude.Messages[s.Claude.Message]
			if msg == nil || msg.Complete {
				return unknown()
			}
			msg.StreamedText = true
			msg.Text += str(block["text"])
			s.claudeAssistant(s.Claude.Message, false, emit)
			return local()
		case "content_block_delta":
			delta := obj(event["delta"])
			if delta["type"] != "text_delta" || s.Claude.Blocks[strNumber(event["index"])] != "text" {
				return unknown()
			}
			msg := s.Claude.Messages[s.Claude.Message]
			if msg == nil || msg.Complete {
				return unknown()
			}
			text, ok := delta["text"].(string)
			if !ok {
				return unknown()
			}
			msg.Text += text
			emit("message/assistant/delta", object{"message_id": s.Claude.Message, "turn_id": msg.Turn, "text": text})
			return local()
		case "content_block_stop":
			if s.Claude.Blocks[strNumber(event["index"])] != "text" {
				return unknown()
			}
			return local()
		case "message_delta":
			msg := s.Claude.Messages[s.Claude.Message]
			if msg == nil {
				return unknown()
			}
			if msg.Usage == nil {
				msg.Usage = object{}
			}
			for k, v := range obj(event["usage"]) {
				msg.Usage[k] = v
			}
			emit("usage/context", object{"turn_id": msg.Turn, "context": object{"used_tokens": nil, "capacity_tokens": nil}, "cumulative": nil, "last_request": claudeTokens(msg.Usage)})
			return local()
		case "message_stop":
			s.claudeAssistant(s.Claude.Message, true, emit)
			return local()
		default:
			return unknown()
		}
	case "assistant":
		if m["parent_tool_use_id"] != nil {
			return unknown()
		}
		message := obj(m["message"])
		id := str(message["id"])
		if id == "" || s.Claude.Turn == "" {
			return unknown()
		}
		text := ""
		hasText := false
		for _, v := range list(message["content"]) {
			part := obj(v)
			if part["type"] == "thinking" {
				continue
			}
			if part["type"] != "text" {
				return unknown()
			}
			hasText = true
			text += str(part["text"])
		}
		if !s.claudeThinkingRecord(p, message, emit) {
			return unknown()
		}
		if !hasText {
			return local()
		}
		msg := s.Claude.Messages[id]
		if msg == nil {
			msg = &ClaudeMessage{Turn: s.Claude.Turn}
			s.Claude.Messages[id] = msg
		}
		if msg.Complete {
			return "debug/dropped", "Assistant message is already finalized."
		}
		// Native assistant records may describe only a completed content block.
		// The captured stream already contains all blocks in order.
		if !msg.StreamedText {
			msg.Text = text
		}
		s.claudeAssistant(id, false, emit)
		return local()
	case "result":
		// A resume-time task notification can finish without any model turn.
		// It must neither manufacture a turn nor close an unrelated active one.
		if obj(m["origin"])["kind"] == "task-notification" && m["subtype"] == "success" && m["is_error"] == false && number(m["num_turns"]) != nil && numeric(m["num_turns"]) == 0 && m["result"] == "" && number(obj(m["usage"])["output_tokens"]) != nil && numeric(obj(m["usage"])["output_tokens"]) == 0 {
			return "debug/dropped", "Empty task-notification receipt; no model turn ran."
		}
		turn := str(m["user_message_uuid"])
		s.claudeSubmission(p, turn, emit)
		if turn == "" {
			turn = s.Claude.Turn
		}
		if turn == "" {
			return unknown()
		}
		if s.Turns[turn] == "completed" {
			return "debug/dropped", "Turn completion was already recorded."
		}
		ids := []string{}
		for id, msg := range s.Claude.Messages {
			if msg.Turn == turn && !msg.Complete {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			s.claudeAssistant(id, true, emit)
		}
		outcome := "completed"
		var failure any
		if m["is_error"] == true || m["subtype"] != "success" {
			outcome = "failed"
			text := str(m["result"])
			if text == "" {
				parts := []string{}
				for _, v := range list(m["errors"]) {
					if str(v) != "" {
						parts = append(parts, str(v))
					}
				}
				text = strings.Join(parts, "; ")
				if text == "" {
					text = str(m["subtype"])
				}
			}
			failure = providerError(text)
		}
		if s.Claude.Cancelled[turn] || m["terminal_reason"] == "aborted_streaming" || m["terminal_reason"] == "aborted_tools" {
			outcome = "interrupted"
		}
		emit("turn/completed", object{"turn_id": turn, "outcome": outcome, "error": failure, "started_at": stamp(m["request_sent_wall_ms"], true), "completed_at": nil, "duration_ms": number(m["duration_ms"])})
		s.Turns[turn] = "completed"
		s.Claude.Compaction = ""
		s.Claude.Turn = turn
		state := "idle"
		if numeric(m["queued_turn_count"]) > 0 {
			state = "running"
		}
		s.claudeStatus(state, emit)
		if fast := str(m["fast_mode_state"]); fast == "on" || fast == "off" || fast == "cooldown" {
			s.claudeConfig(object{"fast_mode": fast == "on"}, emit)
		}
		return local()
	case "rate_limit_event":
		info := obj(m["rate_limit_info"])
		unified := obj(info["unifiedWindows"])
		windows := []any{}
		for _, key := range []string{"five_hour", "seven_day"} {
			w := obj(unified[key])
			if number(w["utilization"]) == nil {
				continue
			}
			duration := 300
			if key == "seven_day" {
				duration = 10080
			}
			windows = append(windows, object{"name": strings.ReplaceAll(key, "_", " "), "used_percent": numeric(w["utilization"]) * 100, "duration_minutes": duration, "resets_at": stamp(w["resetsAt"], false)})
		}
		if len(windows) == 0 {
			return unknown()
		}
		s.claudeAllowance(windows, nil, false, emit)
		return local()
	default:
		return unknown()
	}
}
func (s *State) claudeAssistant(id string, complete bool, emit func(string, object)) {
	msg := s.Claude.Messages[id]
	if msg == nil || msg.Complete || (!msg.StreamedText && msg.Text == "") {
		return
	}
	state := "in_progress"
	if complete {
		state = "completed"
		msg.Complete = true
	}
	// Claude has no commentary/final-answer distinction; never guess it from position.
	emit("message/assistant", object{"message_id": id, "turn_id": msg.Turn, "state": state, "started_at": nil, "completed_at": nil, "text": msg.Text, "phase": nil})
	if complete {
		msg.Text = ""
		msg.Usage = nil
	}
}
func claudeTokens(m object) any {
	if m == nil {
		return nil
	}
	input := number(m["input_tokens"])
	cached := number(m["cache_read_input_tokens"])
	written := number(m["cache_creation_input_tokens"])
	output := number(m["output_tokens"])
	var total, allInput any
	if input != nil && cached != nil && written != nil {
		n := numeric(input) + numeric(cached) + numeric(written)
		allInput = n
		if output != nil {
			total = n + numeric(output)
		}
	}
	return object{"total_tokens": total, "input_tokens": allInput, "cached_input_tokens": cached, "cache_write_input_tokens": written, "output_tokens": output, "reasoning_output_tokens": nil}
}
func (s *State) claudeAccount(r object, emit func(string, object)) {
	limits := obj(r["rate_limits"])
	if limits == nil {
		return
	}
	windows := []any{}
	for _, key := range []string{"five_hour", "seven_day"} {
		w := obj(limits[key])
		if number(w["utilization"]) == nil {
			continue
		}
		duration := 300
		if key == "seven_day" {
			duration = 10080
		}
		windows = append(windows, object{"name": strings.ReplaceAll(key, "_", " "), "used_percent": w["utilization"], "duration_minutes": duration, "resets_at": iso(w["resets_at"])})
	}
	for _, v := range list(limits["model_scoped"]) {
		w := obj(v)
		if number(w["utilization"]) == nil {
			continue
		}
		windows = append(windows, object{"name": nullable(w["display_name"]), "used_percent": w["utilization"], "duration_minutes": 10080, "resets_at": iso(w["resets_at"])})
	}
	s.claudeAllowance(windows, nullable(r["subscription_type"]), true, emit)
}

// A rate-limit event may cover only the general windows. Preserve model-scoped
// windows from the last full account snapshot rather than flickering gauges.
func (s *State) claudeAllowance(windows []any, plan any, replace bool, emit func(string, object)) {
	if !replace && s.Claude.Account != nil {
		existing := append([]any(nil), list(s.Claude.Account["windows"])...)
		for _, v := range windows {
			found := false
			for i, old := range existing {
				if obj(old)["name"] == obj(v)["name"] {
					existing[i] = v
					found = true
					break
				}
			}
			if !found {
				existing = append(existing, v)
			}
		}
		windows = existing
		plan = s.Claude.Account["plan_name"]
	}
	s.Claude.Account = object{"bucket_id": "claude", "bucket_name": "Claude", "windows": windows, "credits": nil, "plan_name": plan}
	emit("usage/account", s.Claude.Account)
}
