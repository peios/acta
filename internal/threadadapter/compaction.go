package threadadapter

import (
	"acta/internal/threads"
	"time"
)

// Compaction keeps provider correlation local; UI records are full snapshots.
// Claude's summary is correlated by the boundary's anchor UUID, never its text.
type Compaction struct {
	ID        string
	Turn      string
	Started   time.Time
	Completed bool
	Anchor    string
	Data      object
}

func (s *State) startCompaction(p threads.ProviderFrame, id, turn string, emit func(string, object)) *Compaction {
	if s.Compactions == nil {
		s.Compactions = map[string]*Compaction{}
	}
	if c := s.Compactions[id]; c != nil {
		return c
	}
	c := &Compaction{ID: id, Turn: turn, Started: p.ReceivedAt}
	c.Data = object{"compaction_id": id, "turn_id": nullable(turn), "state": "in_progress", "started_at": p.ReceivedAt, "completed_at": nil, "duration_ms": nil, "before_tokens": nil, "after_tokens": nil, "summary": nil}
	s.Compactions[id] = c
	emit("context/compaction", c.Data)
	return c
}
func (c *Compaction) complete(p threads.ProviderFrame, emit func(string, object)) {
	if c.Completed {
		return
	}
	c.Completed = true
	c.Data["state"] = "completed"
	c.Data["completed_at"] = p.ReceivedAt
	if c.Data["duration_ms"] == nil && !c.Started.IsZero() {
		c.Data["duration_ms"] = max(int64(0), p.ReceivedAt.Sub(c.Started).Milliseconds())
	}
	emit("context/compaction", c.Data)
}
func (s *State) claudeCompaction(p threads.ProviderFrame, m object, emit func(string, object)) bool {
	if m["parent_tool_use_id"] != nil {
		return false
	}
	if m["type"] == "system" && m["subtype"] == "status" && m["status"] == "compacting" {
		id := str(m["uuid"])
		if id == "" {
			return false
		}
		if c := s.Compactions[s.Claude.Compaction]; c != nil && !c.Completed {
			return true
		}
		s.Claude.Compaction = id
		s.startCompaction(p, id, s.Claude.Turn, emit)
		return true
	}
	if m["type"] == "system" && m["subtype"] == "compact_boundary" {
		id := str(m["uuid"])
		meta := obj(m["compact_metadata"])
		if id == "" || (meta["trigger"] != "manual" && meta["trigger"] != "auto") {
			return false
		}
		// Boundary identity also suppresses repeats after the pending start closes.
		if s.Claude.CompactionBoundaries[id] != "" {
			return true
		}
		c := s.Compactions[s.Claude.Compaction]
		if c == nil || c.Completed {
			c = s.startCompaction(p, id, s.Claude.Turn, func(string, object) {})
			c.Started = time.Time{}
			c.Data["started_at"] = nil
		}
		if s.Claude.CompactionBoundaries == nil {
			s.Claude.CompactionBoundaries = map[string]string{}
		}
		s.Claude.CompactionBoundaries[id] = c.ID
		c.Anchor = str(obj(meta["preserved_segment"])["anchor_uuid"])
		if c.Anchor == "" {
			c.Anchor = str(obj(meta["preserved_messages"])["anchor_uuid"])
		}
		c.Data["before_tokens"] = number(meta["pre_tokens"])
		c.Data["after_tokens"] = number(meta["post_tokens"])
		c.Data["duration_ms"] = number(meta["duration_ms"])
		c.complete(p, emit)
		return true
	}
	if m["type"] == "user" && m["isSynthetic"] == true && obj(m["message"])["role"] == "user" {
		id := str(m["uuid"])
		text, ok := obj(m["message"])["content"].(string)
		if id == "" || !ok {
			return false
		}
		for _, c := range s.Compactions {
			if c.Anchor == id && c.Completed {
				if c.Data["summary"] == nil {
					c.Data["summary"] = text
					emit("context/compaction", c.Data)
				}
				return true
			}
		}
	}
	return false
}
func (s *State) codexCompaction(p threads.ProviderFrame, method string, params object, emit func(string, object)) bool {
	if method != "item/started" && method != "item/completed" {
		return false
	}
	item := obj(params["item"])
	if item["type"] != "contextCompaction" || str(item["id"]) == "" || str(params["turnId"]) == "" {
		return false
	}
	id := str(item["id"])
	c := s.Compactions[id]
	if c == nil {
		if method == "item/started" {
			s.startCompaction(p, id, str(params["turnId"]), emit)
			return true
		}
		c = s.startCompaction(p, id, str(params["turnId"]), func(string, object) {})
		c.Started = time.Time{}
		c.Data["started_at"] = nil
	}
	if method == "item/completed" {
		c.complete(p, emit)
	}
	return true
}
