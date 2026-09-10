package conversation

import (
	"acta2/internal/threads"
	"encoding/json"
)

func (r *Reducer) turn(f threads.Frame, d Object) error {
	turn := str(d["turn_id"])
	if turn == "" {
		return nil
	}
	i, e := r.item(f, key("turn", f.RunID, turn), "turn-ended")
	if e != nil {
		return e
	}
	p := i.Payload
	if p["label"] == nil {
		i.Visibility = "hidden"
		p["label"] = "Turn ended"
		p["durationMs"] = nil
		p["tokens"] = []any{}
		p["diff"] = nil
		p["error"] = ""
	}
	switch f.Kind {
	case "usage/context":
		tokens := []any{}
		call := obj(d["last_request"])
		for _, field := range [][2]string{{"total_tokens", "Total tokens"}, {"input_tokens", "Input tokens"}, {"cached_input_tokens", "Cached input"}, {"cache_write_input_tokens", "Cache writes"}, {"output_tokens", "Output tokens"}, {"reasoning_output_tokens", "Reasoning tokens"}} {
			if n, ok := call[field[0]].(json.Number); ok {
				value, e := n.Float64()
				if e == nil && value >= 0 {
					tokens = append(tokens, Object{"label": field[1], "value": n})
				}
			}
		}
		p["tokens"] = tokens
	case "turn/diff":
		p["diff"] = d["diff"]
		p["diffMode"] = d["mode"]
		p["diffIncomplete"] = d["incomplete"]
	case "turn/completed":
		if i.Visibility != "normal" {
			i.Visibility = "normal"
			i.Sequence = f.Sequence
			i.OutputIndex = f.OutputIndex
		}
		// Later provider lifecycle events can clarify an earlier successful
		// result. Keep one divider at its original position and retain timing.
		prior := obj(obj(p["frame"])["data"])
		merged := Object{}
		for k, v := range prior {
			merged[k] = v
		}
		for k, v := range d {
			if v != nil {
				merged[k] = v
			}
		}
		if prior["outcome"] == "interrupted" {
			merged["outcome"] = "interrupted"
		}
		d = merged
		frame := frameObject(f)
		frame["data"] = d
		p["frame"] = frame
		if d["outcome"] == "failed" {
			p["label"] = "Turn failed"
		} else if d["outcome"] == "interrupted" {
			p["label"] = "Turn interrupted"
		}
		if d["duration_ms"] != nil {
			p["durationMs"] = d["duration_ms"]
		}
		if d["completed_at"] != nil {
			p["completed_at"] = d["completed_at"]
		}
		if p["completed_at"] == nil {
			p["completed_at"] = f.ReceivedAt
		}
		p["error"] = str(obj(d["error"])["message"])
		if p["error"] == "" && d["error"] != nil {
			p["error"] = pretty(d["error"])
		}
	}
	return r.save(i, f)
}
func (r *Reducer) mcp(f threads.Frame, d Object) error {
	c := r.Current
	name := str(d["server_name"])
	status := str(d["status"])
	batchID := c.MCP[name]
	var batch *Item
	var err error
	if batchID != "" {
		batch, err = r.Store.Get(batchID)
		if err != nil {
			return err
		}
	}
	if status == "starting" {
		if batch != nil {
			return nil
		}
		delete(c.TerminalMCP, name)
		if c.OpenMCP != "" {
			batch, err = r.Store.Get(c.OpenMCP)
			if err != nil {
				return err
			}
		}
		if batch == nil {
			batch, err = r.item(f, frameKey(f), "tools")
			if err != nil {
				return err
			}
			batch.Payload["servers"] = []any{}
			batch.Payload["stacked"] = false
			batch.Payload["sealed"] = false
			c.OpenMCP = batch.ID
		}
		servers := list(batch.Payload["servers"])
		found := false
		for _, v := range servers {
			server := obj(v)
			if server["name"] == name {
				server["ready"] = false
				found = true
			}
		}
		if !found {
			servers = append(servers, Object{"name": name, "ready": false})
		}
		batch.Payload["servers"] = servers
		if len(servers) > 1 {
			batch.Payload["stacked"] = true
		}
		c.MCP[name] = batch.ID
		batch.Deleted = false
		return r.save(batch, f)
	}
	signature := pretty([]any{status, d["error"], d["failure_reason"]})
	if batch == nil && c.TerminalMCP[name] == signature {
		return nil
	}
	c.TerminalMCP[name] = signature
	delete(c.MCP, name)
	if batch != nil {
		servers := []any{}
		all := true
		for _, v := range list(batch.Payload["servers"]) {
			server := obj(v)
			if server["name"] == name {
				if status != "ready" {
					continue
				}
				server["ready"] = true
			}
			servers = append(servers, server)
			all = all && yes(server["ready"])
		}
		batch.Payload["servers"] = servers
		batch.Deleted = len(servers) == 0
		if all {
			batch.Payload["sealed"] = true
			batch.Payload["completed_at"] = f.ReceivedAt
			if c.OpenMCP == batch.ID {
				c.OpenMCP = ""
			}
		}
		if err = r.save(batch, f); err != nil {
			return err
		}
	}
	if status == "ready" {
		if batch != nil {
			return nil
		}
		i, e := r.item(f, frameKey(f), "tools")
		if e != nil {
			return e
		}
		i.Payload["servers"] = []any{Object{"name": name, "ready": true}}
		i.Payload["stacked"] = false
		i.Payload["sealed"] = true
		i.Payload["completed_at"] = f.ReceivedAt
		return r.save(i, f)
	}
	i, e := r.item(f, frameKey(f), "tool-error")
	if e != nil {
		return e
	}
	p := i.Payload
	p["name"] = name
	p["cancelled"] = status == "cancelled"
	p["message"] = str(obj(d["error"])["message"])
	p["reason"] = str(d["failure_reason"])
	return r.save(i, f)
}
