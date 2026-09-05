package agentsession

import (
	"path"
	"strconv"
	"strings"

	"github.com/peios/acta/internal/agentsession/model"
)

// Alerts are the moments a session's owner should hear about when they are not
// looking at it: the agent is blocked on them (a permission, a question, a
// plan, an MCP elicitation), or the session stopped (a turn ended, an error,
// the process died). Everything else is just the transcript moving. They are
// read off the model events, so every backend rings the same bell.

// alertFor classifies one projected event. verb is a short stable key (drives
// glyphs and tests), summary the phrase the bell shows after the agent's name.
func alertFor(e model.Event) (verb, summary string, ok bool) {
	d := e.Data
	s := func(k string) string {
		if d == nil {
			return ""
		}
		v, _ := d[k].(string)
		return v
	}
	switch e.T {
	case model.ApprovalRequest:
		if b, _ := d["auto"].(bool); b {
			return "", "", false // the backend's own reviewer is deciding it
		}
		switch s("kind") {
		case "elicitation":
			return "elicitation", "needs input for " + firstNonEmpty(s("server"), "an MCP server") + clip(s("message"), 80), true
		case "question":
			q := ""
			if qs, _ := d["questions"].([]any); len(qs) > 0 {
				if qm, _ := qs[0].(map[string]any); qm != nil {
					q, _ = qm["question"].(string)
				}
			}
			return "question", "has a question" + clip(q, 100), true
		case "plan":
			return "plan", "wants approval for a plan", true
		case "tool":
			name := firstNonEmpty(s("display"), s("tool"), "a tool")
			detail := s("description")
			if in, _ := d["input"].(map[string]any); in != nil {
				cmd, _ := in["command"].(string)
				fp, _ := in["file_path"].(string)
				desc, _ := in["description"].(string)
				if fp != "" {
					fp = path.Base(fp)
				}
				detail = firstNonEmpty(cmd, fp, desc, detail)
			}
			return "permission", "needs permission for " + name + clip(detail, 80), true
		}
		return "", "", false
	case model.TurnEnd:
		if okv, _ := d["ok"].(bool); !okv {
			if b, _ := d["interrupted"].(bool); b {
				return "", "", false // the owner stopped it
			}
			return "failed", "stopped on an error" + clip(s("error"), 80), true
		}
		return "turn_ended", "finished a turn" + clip(s("result"), 100), true
	case model.SessionExit:
		code := 0
		switch v := d["code"].(type) {
		case int:
			code = v
		case float64:
			code = int(v)
		}
		if b, _ := d["expected"].(bool); b || code == 0 {
			return "", "", false
		}
		return "exited", "exited with code " + strconv.Itoa(code), true
	case model.SessionSpawnError:
		return "failed", "couldn't start" + clip(s("error"), 100), true
	case model.SessionResumeFail:
		return "failed", "couldn't resume the conversation and started fresh", true
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
