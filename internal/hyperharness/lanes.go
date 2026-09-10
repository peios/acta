package hyperharness

import (
	"acta2/internal/threads"
	"context"
	"time"
)

// The client chooses an Acta lane, never an arbitrary provider session. Only
// children attributed by this run's adapter can receive native input.
func laneTarget(r *localThread, q threads.Control) (*localThread, error) {
	if q.LaneID == "" {
		return r, nil
	}
	l := r.Adapter.Lanes[q.LaneID]
	if l == nil || l.State.RunID != r.Thread.RunID || q.RunID != r.Thread.RunID {
		return nil, &providerRPCError{"This subagent is unavailable in the current run."}
	}
	if q.Action != "send" && q.Action != "interrupt" {
		return nil, &providerRPCError{"This command is unavailable for subagents."}
	}
	if q.Action == "send" {
		return nil, &providerRPCError{"This provider does not expose direct input to its subagents."}
	}
	copy := *r
	if r.Thread.Provider == "codex" {
		copy.Thread.ProviderID = l.Native
	}
	return &copy, nil
}

// Schedule one read-only metadata snapshot per discovered Codex child/run. The
// pipe journals its stable RPC ID; reconnect cannot create duplicate requests.
// Called with the controller lock held; parsing remains in the capture adapter.
func (c *Controller) probeLaneMetadata(r *localThread) {
	if r.Thread.Provider != "codex" || r.Thread.State != "running" {
		return
	}
	for id, l := range r.Adapter.Lanes {
		key := r.Thread.RunID + "/lane/" + id
		if l.State.RunID != r.Thread.RunID || l.MetadataRead || c.probing[key] {
			continue
		}
		c.probing[key] = true
		copy := *r
		native := l.Native
		c.wg.Go(func() {
			defer func() { c.mu.Lock(); delete(c.probing, key); c.mu.Unlock() }()
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			defer cancel()
			_, _ = c.rpcID(ctx, &copy, "thread/read", "thread/read/lane/"+id, map[string]any{"threadId": native, "includeTurns": false})
		})
	}
}
