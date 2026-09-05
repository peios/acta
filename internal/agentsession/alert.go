package agentsession

import (
	"encoding/json"
	"path"
	"strconv"
	"strings"
)

// Alerts are the moments a session's owner should hear about when they are not
// looking at it: Claude is blocked on them (a permission, a question, a plan,
// an MCP elicitation), or the session stopped (a turn ended, an error, the
// process died). Everything else is just the transcript moving.

// alertFor classifies one stored frame. verb is a short stable key (drives
// glyphs and tests), summary the phrase the bell shows after "Claude".
func alertFor(kind string, payload json.RawMessage) (verb, summary string, ok bool) {
	switch kind {
	case "control_request":
		var p struct {
			Request struct {
				Subtype  string `json:"subtype"`
				ToolName string `json:"tool_name"`
				Display  string `json:"display_name"`
				Server   string `json:"mcp_server_name"`
				Message  string `json:"message"`
				Desc     string `json:"description"`
				Input    struct {
					Command   string `json:"command"`
					FilePath  string `json:"file_path"`
					Desc      string `json:"description"`
					Questions []struct {
						Question string `json:"question"`
					} `json:"questions"`
				} `json:"input"`
			} `json:"request"`
		}
		if json.Unmarshal(payload, &p) != nil {
			return "", "", false
		}
		r := p.Request
		switch r.Subtype {
		case "elicitation":
			return "elicitation", "needs input for " + firstNonEmpty(r.Server, "an MCP server") + clip(r.Message, 80), true
		case "can_use_tool":
			switch r.ToolName {
			case "AskUserQuestion":
				q := ""
				if len(r.Input.Questions) > 0 {
					q = r.Input.Questions[0].Question
				}
				return "question", "has a question" + clip(q, 100), true
			case "ExitPlanMode":
				return "plan", "wants approval for a plan", true
			case "":
				return "", "", false
			}
			name := firstNonEmpty(r.Display, r.ToolName)
			detail := firstNonEmpty(r.Input.Command, path.Base(r.Input.FilePath), r.Input.Desc, r.Desc)
			return "permission", "needs permission for " + name + clip(detail, 80), true
		}
	case "result":
		var p struct {
			Subtype string `json:"subtype"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Errors  []any  `json:"errors"`
		}
		if json.Unmarshal(payload, &p) != nil {
			return "", "", false
		}
		if p.IsError || strings.HasPrefix(p.Subtype, "error") {
			return "failed", "stopped on an error (" + strings.ReplaceAll(strings.TrimPrefix(p.Subtype, "error_"), "_", " ") + ")", true
		}
		return "turn_ended", "finished a turn" + clip(p.Result, 100), true
	case "state":
		var p struct {
			State string `json:"state"`
			Code  int    `json:"code"`
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &p) != nil {
			return "", "", false
		}
		switch p.State {
		case "exit":
			if p.Code != 0 {
				return "exited", "exited with code " + itoa(p.Code), true
			}
		case "spawn_error":
			return "failed", "couldn't start" + clip(p.Error, 100), true
		case "resume_failed":
			return "failed", "couldn't resume the conversation and started fresh", true
		}
	}
	return "", "", false
}

// clip appends s as a short ": …" detail, trimmed to one line of at most n runes.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return ": " + s
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }
