package threadadapter

import (
	"acta2/internal/threads"
	"strings"
)

func (s *State) convertCodex(p threads.ProviderFrame, m object, emit func(string, object)) (string, string) {
	if s.approval(p, m, emit) {
		return "debug/local", "Handled permission request lifecycle."
	}
	method := str(m["method"])
	params := obj(m["params"])
	switch method {
	case "hook/started", "hook/completed", "thread/settings/updated", "thread/status/changed", "thread/tokenUsage/updated", "turn/started", "turn/completed", "item/started", "item/completed", "item/agentMessage/delta", "mcpServer/startupStatus/updated":
		if str(params["threadId"]) == "" {
			return "debug/unknown", "Missing provider thread identity."
		}
	}

	if native := str(params["threadId"]); native != "" && native != s.NativeID {
		return "debug/unknown", "Provider thread identity does not match this Acta thread."
	}
	if s.codexCompaction(p, method, params, emit) {
		return "debug/local", "Handled context compaction lifecycle."
	}
	if codexApprovalReview(method, params, emit) {
		return "debug/local", "Handled automatic permission review."
	}
	if s.codexFileChanges(p, method, params, emit) {
		return "debug/local", "Handled file changes or turn diff."
	}
	if s.codexMCPTool(p, method, params, emit) {
		return "debug/local", "Handled MCP tool lifecycle."
	}
	if s.codexTool(p, method, params, emit) {
		return "debug/local", "Handled tool call lifecycle."
	}
	if s.codexThinking(p, method, params, emit) {
		return "debug/local", "Handled thinking lifecycle."
	}
	if method == "hook/started" || method == "hook/completed" {
		if codexHook(params, method == "hook/completed", emit) {
			return "debug/local", "Handled provider hook lifecycle."
		}
		return "debug/unknown", "Unrecognized hook response shape."
	}
	if method == "item/commandExecution/terminalInteraction" {
		// An empty stdin interaction is a poll of a known shell process. It
		// introduces no input or output to display. Non-empty input stays unknown.
		t := s.tool(str(params["itemId"]))
		if params["stdin"] == "" && str(params["threadId"]) != "" &&
			str(params["processId"]) != "" && t != nil && t.Name == "Shell" &&
			t.Turn == str(params["turnId"]) {
			return "debug/dropped", "Empty terminal poll; command output and completion are separate events."
		}
	}
	if method == "remoteControl/status/changed" {
		return "debug/dropped", "Provider remote-control status is unrelated to Acta control."
	}
	if method == "guardianWarning" {
		return "debug/dropped", "Guardian warning prose is retained for debugging; structured automatic reviews provide the conversation UI."
	}
	if method == "thread/started" {
		if str(obj(params["thread"])["id"]) != s.NativeID {
			return "debug/unknown", "Unexpected provider thread identity."
		}
		return "debug/local", "Provider readiness notification; configuration is read from the RPC response."
	}
	if method == "" {
		id := str(m["id"])
		if !strings.HasPrefix(id, p.RunID+"/") {
			return "debug/unknown", "Unrecognized RPC response identity."
		}
		call := strings.TrimPrefix(id, p.RunID+"/")
		result := obj(m["result"])
		knownCall := strings.HasPrefix(call, "turn/steer/") || strings.HasPrefix(call, "thread/name/set/") || strings.HasPrefix(call, "thread/compact/start/") || call == "initialize" || call == "thread/start" || call == "thread/resume" || call == "turn/start" || strings.HasPrefix(call, "turn/start/") || strings.HasPrefix(call, "turn/interrupt/") || strings.HasPrefix(call, "thread/read/") || strings.HasPrefix(call, "model/list/") || strings.HasPrefix(call, "thread/settings/update/")
		if !knownCall {
			return "debug/unknown", "Unrecognized provider RPC response."
		}
		if m["error"] != nil {
			return "debug/local", "Provider RPC failure is handled by lifecycle control."
		}
		switch {
		case strings.HasPrefix(call, "thread/compact/start/"):
			return "debug/local", "Compaction acknowledgement; lifecycle arrives in provider items."
		case call == "initialize":
			return "debug/local", "Provider handshake and runtime metadata."
		case call == "thread/start" || call == "thread/resume":
			native := str(obj(result["thread"])["id"])
			if native == "" || s.NativeID != "" && native != s.NativeID {
				return "debug/unknown", "Unexpected provider thread identity."
			}
			s.NativeID = native
			s.config(result, emit)
			if status := str(obj(obj(result["thread"])["status"])["type"]); status == "idle" || status == "active" {
				emit("thread/status", object{"status": status, "waiting_for": []string{}})
			}
			return "debug/local", "Resolved configuration; provider identity and resume history remain local."
		case strings.HasPrefix(call, "turn/steer/"):
			return "debug/local", "Mid-turn input acknowledgement; user message arrives in provider items."
		case strings.HasPrefix(call, "turn/interrupt/"):
			return "debug/local", "Turn interrupt acknowledgement; completion arrives in provider events."
		case strings.HasPrefix(call, "thread/name/set/"):
			return "debug/local", "Native title rename acknowledgement."
		case strings.HasPrefix(call, "model/list/"):
			return "debug/local", "Model catalogue is used by the settings controls."
		case strings.HasPrefix(call, "thread/settings/update/"):
			return "debug/local", "Settings command acknowledgement; confirmed configuration arrives in its notification."
		case strings.HasPrefix(call, "thread/read/"):
			return "debug/local", "Provider state probe handled by the hyperharness."
		case call == "turn/start" || strings.HasPrefix(call, "turn/start/"):
			return s.turn(obj(result["turn"]), false, emit)
		default:
			return "debug/unknown", "Unrecognized provider RPC response."
		}
	}
	switch method {
	case "thread/name/updated":
		if str(params["threadId"]) != s.NativeID {
			return "debug/unknown", "Unexpected provider thread identity."
		}
		return "debug/local", "Native title metadata; Acta names are published through discovery."
	case "thread/settings/updated":
		settings := obj(params["threadSettings"])
		if settings == nil || str(settings["model"]) == "" {
			return "debug/unknown", "Missing provider settings."
		}
		normalized := object{}
		for k, v := range settings {
			normalized[k] = v
		}
		normalized["reasoningEffort"] = settings["effort"]
		normalized["sandbox"] = settings["sandboxPolicy"]
		s.config(normalized, emit)
		return "debug/local", "Recorded the provider's effective configuration."
	case "turn/started":
		return s.turn(obj(params["turn"]), false, emit)
	case "turn/completed":
		return s.turn(obj(params["turn"]), true, func(kind string, data object) {
			// Mark surviving processes before the reducer closes unfinished turn items.
			s.codexBackground(str(data["turn_id"]), emit)
			emit(kind, data)
		})
	case "thread/status/changed":
		status := obj(params["status"])
		v := str(status["type"])
		waiting := []string{}
		switch v {
		case "active":
			for _, f := range list(status["activeFlags"]) {
				switch f {
				case "waitingOnApproval":
					waiting = append(waiting, "approval")
				case "waitingOnUserInput":
					waiting = append(waiting, "user_input")
				default:
					return "debug/unknown", "Unrecognized active thread flag."
				}
			}
		case "idle":
		case "systemError":
			v = "error"
		case "notLoaded":
			v = "unknown"
		default:
			return "debug/unknown", "Unrecognized thread status."
		}
		emit("thread/status", object{"status": v, "waiting_for": waiting})
	case "mcpServer/startupStatus/updated":
		if str(params["name"]) == "" {
			return "debug/unknown", "MCP status has no server name."
		}
		emit("mcp/server/status", object{"server_name": params["name"], "status": params["status"], "error": providerError(params["error"]), "failure_reason": nullable(params["failureReason"])})
	case "item/started", "item/completed":
		return s.message(params, method == "item/completed", emit)
	case "item/agentMessage/delta":
		id, turn := str(params["itemId"]), str(params["turnId"])
		text, ok := params["delta"].(string)
		if id == "" || turn == "" || !ok {
			return "debug/unknown", "Unsupported assistant delta shape."
		}
		previous, seen := s.Messages[id]
		if seen && (previous.Turn != turn || previous.Kind != "agentMessage") {
			return "debug/unknown", "Conflicting message identity."
		}
		if previous.Completed || s.Turns[turn] == "completed" {
			return "debug/dropped", "Message or turn is already finalized."
		}
		if !seen {
			s.Messages[id] = Message{Turn: turn, Kind: "agentMessage"}
		}
		emit("message/assistant/delta", object{"message_id": id, "turn_id": turn, "text": text})
	case "thread/tokenUsage/updated":
		usage := obj(params["tokenUsage"])
		if usage == nil {
			return "debug/unknown", "Missing token usage."
		}
		emit("usage/context", object{"turn_id": nullable(params["turnId"]), "context": object{"used_tokens": nil, "capacity_tokens": number(usage["modelContextWindow"])}, "cumulative": tokens(usage["total"]), "last_request": tokens(usage["last"])})
	case "account/rateLimits/updated":
		limits := obj(params["rateLimits"])
		if limits == nil {
			return "debug/unknown", "Missing rate limits."
		}
		windows := []any{}
		for _, key := range []string{"primary", "secondary"} {
			if w := obj(limits[key]); w != nil {
				windows = append(windows, object{"name": nil, "used_percent": w["usedPercent"], "duration_minutes": w["windowDurationMins"], "resets_at": stamp(w["resetsAt"], false)})
			}
		}
		var credits any
		if c := obj(limits["credits"]); c != nil {
			credits = object{"has_credits": c["hasCredits"], "unlimited": c["unlimited"], "balance": c["balance"]}
		}
		emit("usage/account", object{"bucket_id": nullable(limits["limitId"]), "bucket_name": nullable(limits["limitName"]), "windows": windows, "credits": credits, "plan_name": nullable(limits["planType"])})
	default:
		return "debug/unknown", "Provider method has no reviewed Acta mapping."
	}
	return "debug/resolved", "Mapped provider observation to Acta."
}
func (s *State) turn(t object, completed bool, emit func(string, object)) (string, string) {
	id := str(t["id"])
	if id == "" {
		return "debug/unknown", "Missing turn identity."
	}
	if s.Turns[id] == "completed" || !completed && s.Turns[id] == "started" {
		return "debug/dropped", "Turn lifecycle was already recorded."
	}
	if completed {
		outcome := str(t["status"])
		if outcome != "completed" && outcome != "interrupted" && outcome != "failed" {
			return "debug/unknown", "Unsupported terminal turn status."
		}
		emit("turn/completed", object{"turn_id": id, "outcome": outcome, "error": providerError(t["error"]), "started_at": stamp(t["startedAt"], false), "completed_at": stamp(t["completedAt"], false), "duration_ms": number(t["durationMs"])})
		s.Turns[id] = "completed"
	} else {
		emit("turn/started", object{"turn_id": id, "started_at": stamp(t["startedAt"], false)})
		s.Turns[id] = "started"
	}
	return "debug/resolved", "Recorded turn lifecycle; embedded history is not re-imported."
}
func (s *State) message(p object, completed bool, emit func(string, object)) (string, string) {
	item := obj(p["item"])
	kind := str(item["type"])
	id, turn := str(item["id"]), str(p["turnId"])
	if id == "" || turn == "" || (kind != "userMessage" && kind != "agentMessage") {
		return "debug/unknown", "Unreviewed item type or identity."
	}
	previous, seen := s.Messages[id]
	if seen && (previous.Turn != turn || previous.Kind != kind) {
		return "debug/unknown", "Conflicting message identity."
	}
	if previous.Completed || s.Turns[turn] == "completed" {
		return "debug/dropped", "Message or turn is already finalized."
	}
	data := object{"message_id": id, "turn_id": turn, "state": "in_progress", "started_at": previous.Started, "completed_at": nil}
	if completed {
		data["state"] = "completed"
		data["completed_at"] = stamp(p["completedAtMs"], true)
	} else if data["started_at"] == nil {
		data["started_at"] = stamp(p["startedAtMs"], true)
	}
	if kind == "userMessage" {
		submission := previous.Submission
		if client := str(item["clientId"]); client != "" && s.Submissions[client] == s.RunID {
			submission = client
		}
		if submission != "" {
			data["submission_id"] = submission
			previous.Submission = submission
		}
		parts := []any{}
		for _, v := range list(item["content"]) {
			part := obj(v)
			if part["type"] == "image" {
				image, valid := inputImage(part, "codex")
				if !valid {
					return "debug/unknown", "User image is not embedded raster content."
				}
				parts = append(parts, image)
				continue
			}
			if part["type"] == "localImage" && len(s.SubmissionImages[submission]) > 0 {
				continue
			}
			text, ok := part["text"].(string)
			if str(part["type"]) != "text" || !ok || len(list(part["text_elements"])) > 0 {
				return "debug/unknown", "User content includes unreviewed structured content."
			}
			parts = append(parts, object{"type": "text", "text": text})
		}
		if len(s.SubmissionImages[submission]) > 0 {
			parts = inputParts(s.SubmissionTexts[submission], s.SubmissionImages[submission])
		}
		if len(parts) == 0 {
			return "debug/unknown", "Missing user content."
		}
		data["content"] = parts
		emit("message/user", data)
	} else {
		for _, key := range []string{"memoryCitation", "delivery", "questions"} {
			if item[key] != nil {
				return "debug/unknown", "Assistant message contains unreviewed metadata."
			}
		}
		text, ok := item["text"].(string)
		if !ok {
			return "debug/unknown", "Missing assistant text."
		}
		phase := nullable(item["phase"])
		if phase != nil && phase != "commentary" && phase != "final_answer" {
			return "debug/unknown", "Unreviewed assistant phase."
		}
		data["text"] = text
		data["phase"] = phase
		emit("message/assistant", data)
	}
	s.Messages[id] = Message{Turn: turn, Kind: kind, Started: data["started_at"], Completed: completed, Submission: previous.Submission}
	return "debug/resolved", "Mapped full message snapshot."
}
func tokens(v any) any {
	m := obj(v)
	if m == nil {
		return nil
	}
	out := object{}
	for native, key := range map[string]string{"totalTokens": "total_tokens", "inputTokens": "input_tokens", "cachedInputTokens": "cached_input_tokens", "cacheWriteInputTokens": "cache_write_input_tokens", "outputTokens": "output_tokens", "reasoningOutputTokens": "reasoning_output_tokens"} {
		out[key] = number(m[native])
	}
	return out
}
