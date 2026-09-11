package threadadapter

import (
	"acta/internal/threads"
	"strconv"
	"strings"
)

func (s *State) codexThinking(p threads.ProviderFrame, method string, params object, emit func(string, object)) bool {
	item := obj(params["item"])
	lifecycle := (method == "item/started" || method == "item/completed") && item["type"] == "reasoning"
	delta := strings.HasPrefix(method, "item/reasoning/")
	if !lifecycle && !delta {
		return false
	}
	id, turn := str(item["id"]), str(params["turnId"])
	if delta {
		id = str(params["itemId"])
	}
	if id == "" || turn == "" || str(params["threadId"]) != s.NativeID {
		return false
	}
	started := stamp(params["startedAtMs"], true)
	if started == nil && method != "item/completed" {
		started = p.ReceivedAt
	}
	if delta {
		switch method {
		case "item/reasoning/summaryPartAdded", "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		default:
			return false
		}
		index := params["summaryIndex"]
		if method == "item/reasoning/textDelta" {
			index = params["contentIndex"]
		}
		if number(index) == nil || numeric(index) < 0 {
			return false
		}
		if method != "item/reasoning/summaryPartAdded" {
			if _, ok := params["delta"].(string); !ok {
				return false
			}
		}
		t := s.thinking(id, turn, started)
		if t.Done || t.Turn != turn {
			return t.Done && t.Turn == turn
		}
		parts := t.Summary
		if method == "item/reasoning/textDelta" {
			parts = t.Content
		}
		key := strNumber(index)
		parts[key] += str(params["delta"])
		channel := "summary"
		if method == "item/reasoning/textDelta" {
			channel = "content"
		}
		n, _ := strconv.Atoi(key)
		emitThinkingDelta(id, t, str(params["delta"]), channel, n, emit)
		return true
	}
	if method == "item/started" && s.Thinking[id] != nil {
		return s.Thinking[id].Turn == turn
	}
	for _, field := range []string{"summary", "content"} {
		values, ok := item[field].([]any)
		if !ok {
			return false
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
	}
	t := s.thinking(id, turn, started)
	if t.Turn != turn {
		return false
	}
	if t.Done {
		return true
	}
	for field, dest := range map[string]map[string]string{"summary": t.Summary, "content": t.Content} {
		if values, ok := item[field].([]any); ok {
			for k := range dest {
				delete(dest, k)
			}
			for i, v := range values {
				dest[strconv.Itoa(i)] = str(v)
			}
		}
	}
	kind := "thinking/started"
	if method == "item/completed" {
		kind = "thinking/completed"
		t.Done = true
		t.Completed = stamp(params["completedAtMs"], true)
		if t.Completed == nil {
			t.Completed = p.ReceivedAt
		}
	}
	emitThinking(id, kind, t, emit)
	return true
}

// The system estimate is cumulative across a turn. Only attribute it to a
// thinking item while that turn has exactly one item; never split it by guesswork.
