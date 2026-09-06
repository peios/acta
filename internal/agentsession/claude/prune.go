package claude

import (
	"encoding/json"

	"github.com/peios/acta/internal/agentsession/model"
)

// Prune rewrites one stored frame with the chosen categories taken out,
// keeping its shape so the projector still reads it: a tool result becomes
// its marker, a thinking block keeps its type with the text replaced, an
// image block becomes a text block saying so. It returns the frame
// unchanged (and false) when nothing in it belongs to a chosen category.
func Prune(kind string, payload json.RawMessage, cats map[string]bool) (json.RawMessage, bool) {
	switch kind {
	case "assistant", "user", TranscriptKind:
	case "input":
		if !cats[model.PruneImages] {
			return payload, false
		}
		return pruneInput(payload)
	default:
		return payload, false
	}
	m := obj(payload)
	if m == nil {
		return payload, false
	}
	changed := false
	if cats[model.PruneToolOutput] {
		if _, ok := m["toolUseResult"]; ok {
			delete(m, "toolUseResult")
			changed = true
		}
	}
	msg := sub(m, "message")
	if msg == nil {
		if !changed {
			return payload, false
		}
		return marshal(payload, m)
	}
	blocks, isList := msg["content"].([]any)
	if !isList {
		if !changed {
			return payload, false
		}
		return marshal(payload, m)
	}
	for i, b := range blocks {
		bm, _ := b.(map[string]any)
		if bm == nil {
			continue
		}
		switch str(bm, "type") {
		case "tool_result":
			if cats[model.PruneToolOutput] {
				if n := sizeOf(bm["content"]); n > model.PruneKeep {
					bm["content"] = model.Pruned("output", n)
					changed = true
				}
			}
		case "tool_use":
			if cats[model.PruneToolInput] {
				if in, ok := bm["input"].(map[string]any); ok {
					for k, v := range in {
						if s, ok := v.(string); ok && len(s) > model.PruneKeep {
							in[k] = model.Pruned("input", len(s))
							changed = true
						}
					}
				}
			}
		case "thinking":
			if cats[model.PruneThinking] {
				if t := str(bm, "thinking"); len(t) > model.PruneKeep {
					bm["thinking"] = model.Pruned("thinking", len(t))
					delete(bm, "signature")
					changed = true
				}
			}
		case "image":
			if cats[model.PruneImages] {
				n := sizeOf(bm["source"])
				blocks[i] = map[string]any{"type": "text", "text": model.Pruned("image", n)}
				changed = true
			}
		}
	}
	if !changed {
		return payload, false
	}
	return marshal(payload, m)
}

// pruneInput takes the attached images out of one of Acta's own input
// frames, leaving a count the page can mention.
func pruneInput(payload json.RawMessage) (json.RawMessage, bool) {
	m := obj(payload)
	imgs := arr(m, "images")
	if len(imgs) == 0 {
		return payload, false
	}
	delete(m, "images")
	m["images_pruned"] = len(imgs)
	return marshal(payload, m)
}

// sizeOf is roughly how many bytes a value takes in JSON.
func sizeOf(v any) int {
	if s, ok := v.(string); ok {
		return len(s)
	}
	b, _ := json.Marshal(v)
	return len(b)
}

func marshal(orig json.RawMessage, m map[string]any) (json.RawMessage, bool) {
	b, err := json.Marshal(m)
	if err != nil {
		return orig, false
	}
	return b, true
}
