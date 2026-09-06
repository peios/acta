package codex

import (
	"encoding/json"

	"github.com/peios/acta/internal/agentsession/model"
)

// Prune rewrites one stored frame with the chosen categories taken out,
// keeping its shape so the projector still reads it. Items arrive in two
// spellings: the rollout's core shape (a transcript frame's payload.item)
// and the app-server's v2 shape (a live item/completed's params.item); the
// field names differ and both are handled. Unchanged frames come back as
// they were, with false.
func Prune(kind string, payload json.RawMessage, cats map[string]bool) (json.RawMessage, bool) {
	m := obj(payload)
	if m == nil {
		return payload, false
	}
	var it map[string]any
	switch kind {
	case TranscriptKind:
		if str(m, "type") != "event_msg" {
			return payload, false
		}
		pl := sub(m, "payload")
		if str(pl, "type") != "item_completed" {
			return payload, false
		}
		it = sub(pl, "item")
	case "item/completed", "item/started":
		it = sub(sub(m, "params"), "item")
	default:
		return payload, false
	}
	if it == nil {
		return payload, false
	}
	changed := false
	cut := func(key, what string) {
		if s, ok := it[key].(string); ok && len(s) > model.PruneKeep {
			it[key] = model.Pruned(what, len(s))
			changed = true
		}
	}
	switch str(it, "type") {
	case "CommandExecution", "commandExecution":
		if cats[model.PruneToolOutput] {
			for _, k := range []string{"aggregated_output", "formatted_output", "stdout", "stderr", "aggregatedOutput"} {
				cut(k, "output")
			}
		}
	case "McpToolCall", "mcpToolCall":
		if cats[model.PruneToolOutput] {
			if res, ok := it["result"]; ok {
				b, _ := json.Marshal(res)
				if len(b) > model.PruneKeep {
					it["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": model.Pruned("output", len(b))}}}
					changed = true
				}
			}
		}
	case "FileChange", "fileChange":
		if cats[model.PruneToolInput] {
			switch ch := it["changes"].(type) {
			case map[string]any: // core: path -> change
				for _, v := range ch {
					if cm, ok := v.(map[string]any); ok {
						if d, ok := cm["diff"].(string); ok && len(d) > model.PruneKeep {
							cm["diff"] = model.Pruned("diff", len(d))
							changed = true
						}
					}
				}
			case []any: // v2: [{path, diff}]
				for _, v := range ch {
					if cm, ok := v.(map[string]any); ok {
						if d, ok := cm["diff"].(string); ok && len(d) > model.PruneKeep {
							cm["diff"] = model.Pruned("diff", len(d))
							changed = true
						}
					}
				}
			}
		}
	case "Reasoning", "reasoning":
		if cats[model.PruneThinking] {
			for _, k := range []string{"summary_text", "raw_content", "summary", "content"} {
				if xs, ok := it[k].([]any); ok {
					n := 0
					for _, x := range xs {
						if s, ok := x.(string); ok {
							n += len(s)
						}
					}
					if n > model.PruneKeep {
						it[k] = []any{model.Pruned("thinking", n)}
						changed = true
					}
				}
			}
		}
	}
	if !changed {
		return payload, false
	}
	b, err := json.Marshal(m)
	if err != nil {
		return payload, false
	}
	return b, true
}
