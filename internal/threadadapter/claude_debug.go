package threadadapter

import (
	"encoding/json"
	"strings"
)

// Configuration queries are needed locally for readback, but their effective
// settings, sources and MCP transport configuration may contain credentials.
// Keep original capture in the local pipe; explicitly minimize the debug copy
// crossing into Acta. Unrecognized conversation frames remain untouched.
func claudeDebugCopy(source object, run string) (string, bool) {
	if source["type"] != "control_response" {
		return "", false
	}
	response := obj(source["response"])
	id := str(response["request_id"])
	if !strings.HasPrefix(id, run+"/") {
		return "", false
	}
	call := strings.Split(strings.TrimPrefix(id, run+"/"), "/")[0]
	native := obj(response["response"])
	safe := object{}
	switch call {
	case "get_settings":
		applied := obj(native["applied"])
		safe["applied"] = object{"model": applied["model"], "effort": applied["effort"]}
	case "initialize":
		for _, key := range []string{"session_state", "current_permission_mode", "fast_mode_state", "fast_mode_disabled_reason", "models"} {
			if v, ok := native[key]; ok {
				safe[key] = v
			}
		}
	case "mcp_status":
		servers := []any{}
		for _, v := range list(native["mcpServers"]) {
			server := obj(v)
			item := object{}
			for _, key := range []string{"name", "status", "error", "serverInfo"} {
				if value, ok := server[key]; ok {
					item[key] = value
				}
			}
			servers = append(servers, item)
		}
		safe["mcpServers"] = servers
	default:
		return "", false
	}
	result := object{"type": "control_response", "response": object{"request_id": id, "subtype": response["subtype"], "response": safe}}
	// Error strings from configuration parsing can quote secret values.
	if response["subtype"] == "error" {
		obj(result["response"])["error"] = "Local provider configuration query failed; details withheld."
	}
	raw, _ := json.Marshal(result)
	return string(raw), true
}
