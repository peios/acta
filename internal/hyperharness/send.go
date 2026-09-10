package hyperharness

import (
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"errors"
)

type providerRPCError struct{ message string }

func (e *providerRPCError) Error() string { return e.message }

// Sending owns the same per-thread operation slot as lifecycle control, but a
// failed message must never kill an otherwise healthy provider process.
func (c *Controller) executeSend(ctx context.Context, r *localThread) {
	q := r.Requests[r.CommandID]
	target, err := laneTarget(r, q)
	if err != nil {
		c.finishOperation(r, threads.Result{ID: q.ID, ThreadID: r.Thread.ID, Outcome: "rejected", Error: err.Error()})
		return
	}
	r = target
	if !r.SendAttempted && len(q.Images) > 0 {
		if err := c.requireImageTransport(ctx, q); err != nil {
			c.finishOperation(r, threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected", Error: err.Error()})
			return
		}
	}
	if r.Thread.Provider == "claude" {
		c.executeClaudeSend(ctx, r, q)
		return
	}
	result := threads.Result{ID: q.ID, ThreadID: r.Thread.ID, Outcome: "rejected"}
	method, turnID, err := c.codexSendTarget(ctx, r, q.ID)
	if err != nil {
		if r.SendAttempted {
			result.Outcome = "uncertain"
		}
		result.Error = err.Error()
		c.finishOperation(r, result)
		return
	}
	if !r.SendAttempted {
		if len(q.Images) > 0 {
			if err := c.requireImageModel(ctx, r, q); err != nil {
				result.Error = err.Error()
				c.finishOperation(r, result)
				return
			}
		}
		if !c.prepareInput(r) {
			return
		}
	}
	params := map[string]any{
		"threadId": r.Thread.ProviderID, "clientUserMessageId": q.ID,
		"input": codexInput(q),
	}
	if turnID != "" {
		params["expectedTurnId"] = turnID
	}
	raw, err := c.rpcID(ctx, r, method, method+"/"+q.ID, params)
	result.Outcome = "accepted"
	if err != nil {
		result.Outcome = "uncertain"
		var rejected *providerRPCError
		if errors.As(err, &rejected) {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
	} else {
		var response struct {
			TurnID string `json:"turnId"`
			Turn   struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(raw, &response) != nil ||
			(method == "turn/start" && response.Turn.ID == "") ||
			(method == "turn/steer" && response.TurnID != turnID) {
			result.Outcome = "uncertain"
			result.Error = "The provider returned no turn identity; message acceptance is uncertain."
		}
	}
	c.finishOperation(r, result)
}

// The read and resulting write have stable journal IDs. Recovery must repeat
// the original decision, never redirect an uncertain send into a later turn.
func (c *Controller) codexSendTarget(ctx context.Context, r *localThread, id string) (string, string, error) {
	raw, err := c.rpcID(ctx, r, "thread/read", "thread/read/send-"+id, map[string]any{"threadId": r.Thread.ProviderID, "includeTurns": false})
	if err != nil {
		return "", "", err
	}
	var reply struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
			Turns []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.Thread.ID != r.Thread.ProviderID {
		return "", "", errors.New("Could not identify the provider thread for this message.")
	}
	if reply.Thread.Status.Type == "idle" {
		return "turn/start", "", nil
	}
	if reply.Thread.Status.Type != "active" {
		return "", "", errors.New("The provider is not ready to receive this message.")
	}
	// Empty Codex sessions reject includeTurns until their first message. Only
	// request turn history after the inexpensive status probe reports activity.
	raw, err = c.rpcID(ctx, r, "thread/read", "thread/read/send-active-"+id, map[string]any{"threadId": r.Thread.ProviderID, "includeTurns": true})
	if err != nil {
		return "", "", err
	}
	reply.Thread.Turns = nil
	if json.Unmarshal(raw, &reply) != nil || reply.Thread.ID != r.Thread.ProviderID {
		return "", "", errors.New("Could not identify the active provider thread.")
	}
	for i := len(reply.Thread.Turns) - 1; i >= 0; i-- {
		turn := reply.Thread.Turns[i]
		if turn.Status == "inProgress" && turn.ID != "" {
			return "turn/steer", turn.ID, nil
		}
	}
	return "", "", errors.New("Could not identify the active turn; try sending again.")
}

func (c *Controller) finishOperation(r *localThread, result threads.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return
	} // Recovery reuses the persisted write ID.
	current := c.records[r.Thread.ID]
	if current.Commands[result.ID].Outcome != "accepted" {
		current.Commands[result.ID] = result
	}
	if r.Action == "configure" && result.Outcome == "accepted" {
		settings := r.Requests[result.ID].Settings
		current.Configuration = &settings
		if result.Settings != nil {
			current.Configuration = result.Settings
		}
	}
	if r.Action == "permissions" && result.Outcome == "accepted" {
		current.PermissionMode = result.PermissionMode
	}
	current.Action = ""
	_ = c.save(current)
}
