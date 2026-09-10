package threadadapter

import (
	"sort"
	"strconv"
	"strings"
)

// Thinking snapshots retain section boundaries and reconcile terminal provider
// records with streamed text. They live in persisted adapter state, not sockets.
type Thinking struct {
	Signature string
	Turn      string
	Started   any
	Completed any
	Done      bool
	Summary   map[string]string
	Content   map[string]string
	Estimate  any
}

func thinkingText(parts map[string]string) string {
	keys := make([]int, 0, len(parts))
	for key := range parts {
		n, _ := strconv.Atoi(key)
		keys = append(keys, n)
	}
	sort.Ints(keys)
	text := make([]string, 0, len(keys))
	for _, key := range keys {
		text = append(text, parts[strconv.Itoa(key)])
	}
	return strings.Join(text, "\n\n")
}
func (s *State) thinking(id, turn string, started any) *Thinking {
	if s.Thinking == nil {
		s.Thinking = map[string]*Thinking{}
	}
	if s.Thinking[id] == nil {
		s.Thinking[id] = &Thinking{Turn: turn, Started: started, Summary: map[string]string{}, Content: map[string]string{}}
	}
	return s.Thinking[id]
}
func emitThinking(id, kind string, t *Thinking, emit func(string, object)) {
	text := thinkingText(t.Summary)
	if text == "" {
		text = thinkingText(t.Content)
	}
	if kind == "thinking/delta" {
		emitThinkingDelta(id, t, "", "content", 0, emit)
		return
	}
	emit(kind, object{"thinking_id": id, "turn_id": t.Turn, "text": text, "estimated_tokens": t.Estimate, "started_at": t.Started, "completed_at": t.Completed})
}
func emitThinkingDelta(id string, t *Thinking, text, channel string, index int, emit func(string, object)) {
	emit("thinking/delta", object{"thinking_id": id, "turn_id": t.Turn, "text": text, "channel": channel, "section_index": index, "estimated_tokens": t.Estimate, "started_at": t.Started, "completed_at": t.Completed})
}
