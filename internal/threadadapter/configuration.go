package threadadapter

import "reflect"

func (s *State) config(r object, emit func(string, object)) {
	config := object{}
	for k, v := range s.Configuration {
		config[k] = v
	}
	for native, key := range map[string]string{"model": "model", "cwd": "cwd", "reasoningEffort": "effort"} {
		if v, ok := r[native]; ok {
			config[key] = v
		}
	}
	if tier, ok := r["serviceTier"]; ok {
		switch tier {
		case "fast", "priority":
			config["fast_mode"] = true
		case "default", "flex":
			config["fast_mode"] = false
		default:
			delete(config, "fast_mode")
		}
	}
	if r["approvalPolicy"] != nil || r["sandbox"] != nil {
		config["permissions"] = permissions(r)
	}
	if len(config) > 0 && !reflect.DeepEqual(config, s.Configuration) {
		s.Configuration = config
		emit("thread/configuration", config)
	}
}
func permissions(r object) object {
	mode, fs, net := "unknown", "unknown", "unknown"
	sandbox := obj(r["sandbox"])
	switch sandbox["type"] {
	case "readOnly":
		fs = "read_only"
		net = "blocked"
	case "workspaceWrite":
		fs = "workspace_write"
		if len(list(sandbox["writableRoots"])) > 0 {
			fs = "custom"
		}
		net = "blocked"
	case "dangerFullAccess":
		fs = "unrestricted"
		net = "allowed"
	case "externalSandbox":
		fs = "custom"
	}
	if allowed, ok := sandbox["networkAccess"].(bool); ok {
		if allowed {
			net = "allowed"
		} else {
			net = "blocked"
		}
	}
	switch r["approvalPolicy"] {
	case "on-request":
		switch r["approvalsReviewer"] {
		case "auto_review":
			mode = "automatic"
		case "user":
			mode = "ask"
		default:
			mode = "unknown"
		}
	case "untrusted":
		if fs == "workspace_write" {
			mode = "accept_edits"
		} else {
			mode = "custom"
		}
	case "never":
		mode = "dont_ask"
		if fs == "unrestricted" {
			mode = "bypass"
		}
	default:
		if r["approvalPolicy"] != nil {
			mode = "custom"
		}
	}
	return object{"mode": mode, "filesystem": fs, "network": net}
}
