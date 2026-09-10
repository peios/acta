package threadadapter

// Hook output is provider-reported data, never interpreted as instructions.
// Keep streams separate: combined output need not be identical to stdout.
func claudeHook(m object, emit func(string, object)) bool {
	if str(m["hook_id"]) == "" || str(m["hook_name"]) == "" || str(m["hook_event"]) == "" {
		return false
	}
	data := object{"hook_id": m["hook_id"], "name": m["hook_name"], "event": m["hook_event"], "turn_id": nil}
	if m["subtype"] == "hook_started" {
		emit("hook/started", data)
		return true
	}
	status := map[string]string{"success": "completed", "error": "failed", "cancelled": "cancelled"}[str(m["outcome"])]
	if status == "" {
		return false
	}
	for _, key := range []string{"output", "stdout", "stderr"} {
		if _, ok := m[key].(string); !ok {
			return false
		}
		data[key] = m[key]
	}
	data["status"] = status
	data["exit_code"] = m["exit_code"]
	data["entries"] = []any{}
	data["status_message"] = nil
	data["duration_ms"] = nil
	emit("hook/completed", data)
	return true
}

func codexHook(params object, completed bool, emit func(string, object)) bool {
	run := obj(params["run"])
	if str(run["id"]) == "" || str(run["eventName"]) == "" {
		return false
	}
	data := object{"hook_id": run["id"], "name": run["eventName"], "event": run["eventName"], "turn_id": nullable(params["turnId"])}
	if !completed {
		emit("hook/started", data)
		return true
	}
	status := map[string]string{"completed": "completed", "failed": "failed", "blocked": "blocked", "stopped": "cancelled"}[str(run["status"])]
	if status == "" {
		return false
	}
	entries, ok := run["entries"].([]any)
	if !ok {
		return false
	}
	for _, entry := range entries {
		e := obj(entry)
		if str(e["kind"]) == "" {
			return false
		}
		if _, ok := e["text"].(string); !ok {
			return false
		}
	}
	data["status"] = status
	data["entries"] = entries
	data["output"], data["stdout"], data["stderr"] = nil, nil, nil
	data["exit_code"] = nil
	data["status_message"] = nullable(run["statusMessage"])
	data["duration_ms"] = number(run["durationMs"])
	emit("hook/completed", data)
	return true
}
