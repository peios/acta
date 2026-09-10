package threadadapter

import (
	"acta2/internal/threads"
	"sort"
	"strconv"
	"strings"
)

func (s *State) claudeThinkingEstimate(turn string, estimate any, emit func(string, object)) bool {
	if number(estimate) == nil || numeric(estimate) < 0 {
		return false
	}
	var only string
	for id, t := range s.Thinking {
		if t.Turn != turn {
			continue
		}
		if only != "" {
			return true
		}
		only = id
	}
	t := s.Thinking[only]
	if t == nil {
		return false
	}
	if t.Done {
		return true
	}
	if t.Estimate == nil || numeric(estimate) > numeric(t.Estimate) {
		t.Estimate = estimate
		emitThinking(only, "thinking/delta", t, emit)
	}
	return true
}
func (s *State) claudeThinking(p threads.ProviderFrame, m object, emit func(string, object)) bool {
	if m["parent_tool_use_id"] != nil {
		return false
	}
	turn := str(m["user_message_uuid"])
	if turn == "" {
		turn = s.Claude.Turn
	}
	if turn == "" {
		return false
	}
	if m["type"] == "system" && m["subtype"] == "thinking_tokens" {
		return s.claudeThinkingEstimate(turn, m["estimated_tokens"], emit)
	}
	if m["type"] != "stream_event" {
		return false
	}
	event := obj(m["event"])
	kind := str(event["type"])
	index := strNumber(event["index"])
	if index == "" || s.Claude.Message == "" {
		return false
	}
	id := s.Claude.Message + "/thinking/" + index
	if kind == "content_block_start" {
		block := obj(event["content_block"])
		if block["type"] != "thinking" {
			return false
		}
		if s.Thinking[id] != nil {
			return true
		}
		s.Claude.Blocks[index] = "thinking"
		t := s.thinking(id, turn, p.ReceivedAt)
		t.Content["0"] = str(block["thinking"])
		// A second thinking block makes the cumulative estimate ambiguous for both.
		for other, old := range s.Thinking {
			if other != id && old.Turn == turn && old.Estimate != nil {
				old.Estimate = nil
				k := "thinking/delta"
				if old.Done {
					k = "thinking/completed"
				}
				emitThinking(other, k, old, emit)
			}
		}
		emitThinking(id, "thinking/started", t, emit)
		return true
	}
	if s.Claude.Blocks[index] != "thinking" {
		return false
	}
	t := s.Thinking[id]
	if t == nil {
		return false
	}
	switch kind {
	case "content_block_delta":
		d := obj(event["delta"])
		if d["type"] == "signature_delta" {
			if !t.Done {
				t.Signature += str(d["signature"])
			}
			return true
		}
		if d["type"] != "thinking_delta" {
			return false
		}
		if t.Done {
			return true
		}
		text, ok := d["thinking"].(string)
		if !ok {
			return false
		}
		if text != "" {
			t.Content["0"] += text
			emitThinkingDelta(id, t, text, "content", 0, emit)
		}
		if d["estimated_tokens"] != nil {
			s.claudeThinkingEstimate(turn, d["estimated_tokens"], emit)
		}
		return true
	case "content_block_stop":
		if !t.Done {
			t.Done = true
			t.Completed = p.ReceivedAt
			emitThinking(id, "thinking/completed", t, emit)
		}
		return true
	}
	return false
}

// A Claude assistant envelope can describe a single block rather than a whole
// message. Match thinking records by their order among thinking blocks, never
// by their position among unrelated text/tool blocks.
func (s *State) claudeThinkingRecord(p threads.ProviderFrame, message object, emit func(string, object)) bool {
	id := str(message["id"])
	blocks := []string{}
	for key := range s.Thinking {
		if strings.HasPrefix(key, id+"/thinking/") {
			blocks = append(blocks, key)
		}
	}
	sort.Slice(blocks, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(blocks[i], id+"/thinking/"))
		b, _ := strconv.Atoi(strings.TrimPrefix(blocks[j], id+"/thinking/"))
		return a < b
	})
	for _, v := range list(message["content"]) {
		part := obj(v)
		if part["type"] != "thinking" {
			continue
		}
		signature := str(part["signature"])
		key := ""
		for _, candidate := range blocks {
			if signature != "" && s.Thinking[candidate].Signature == signature {
				key = candidate
				break
			}
		}
		if key == "" && len(blocks) == 1 && (signature == "" || s.Thinking[blocks[0]].Signature == "") {
			key = blocks[0]
		}
		if key == "" {
			return false
		} // No trustworthy link to a streamed block.
		t := s.Thinking[key]
		t.Content["0"] = str(part["thinking"])
		t.Signature = signature
		t.Done = true
		if t.Completed == nil {
			t.Completed = p.ReceivedAt
		}
		emitThinking(key, "thinking/completed", t, emit)
	}
	return true
}
