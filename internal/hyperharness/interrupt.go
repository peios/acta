package hyperharness

import (
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"errors"
)

// Interrupt is a run-bound input command, not a process lifecycle operation.
// The pipe's durable RPC IDs make recovery replay the original interruption.
func (c *Controller) executeInterrupt(ctx context.Context, r *localThread) {
	q := r.Requests[r.CommandID]
	result := threads.Result{ID: q.ID, ThreadID: r.Thread.ID, Outcome: "accepted"}
	target, err := laneTarget(r, q)
	if err == nil {
		err = c.interrupt(ctx, target, q.ID)
	}
	if err != nil {
		result.Outcome = "uncertain"
		var rejected *providerRPCError
		if errors.As(err, &rejected) {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
	}
	c.finishOperation(r, result)
}

func (c *Controller) interrupt(ctx context.Context, r *localThread, id string) error {
	if r.Thread.Provider == "claude" {
		if lane := r.Requests[id].LaneID; lane != "" {
			_, err := c.rpcID(ctx, r, "stop_task", "stop_task/"+id, map[string]any{"task_id": lane})
			return err
		}
		_, err := c.rpcID(ctx, r, "interrupt", "interrupt/"+id, map[string]any{})
		return err
	}
	// Read once under a stable command ID. Recovery must never retarget a later
	// turn: rpcID returns the same journalled read response on replay.
	raw, err := c.rpcID(ctx, r, "thread/read", "thread/read/interrupt-"+id, map[string]any{"threadId": r.Thread.ProviderID, "includeTurns": true})
	if err != nil {
		return err
	}
	var reply struct {
		Thread struct {
			ID    string `json:"id"`
			Turns []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.Thread.ID != r.Thread.ProviderID {
		return &providerRPCError{"Could not identify the active turn in this provider session."}
	}
	for i := len(reply.Thread.Turns) - 1; i >= 0; i-- {
		turn := reply.Thread.Turns[i]
		if turn.Status == "inProgress" && turn.ID != "" {
			_, err = c.rpcID(ctx, r, "turn/interrupt", "turn/interrupt/"+id, map[string]any{"threadId": r.Thread.ProviderID, "turnId": turn.ID})
			return err
		}
	}
	return &providerRPCError{"There is no active turn to stop."}
}
