package hyperharness

import (
	"acta2/internal/harnesspipe"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (c *Controller) rpc(ctx context.Context, r *localThread, method string, params any) (json.RawMessage, error) {
	return c.rpcID(ctx, r, method, method, params)
}
func (c *Controller) rpcID(ctx context.Context, r *localThread, method, suffix string, params any) (json.RawMessage, error) {
	id := r.Thread.RunID + "/" + suffix
	raw, err := providerRequest(r.Thread.Provider, id, method, params)
	if err != nil {
		return nil, err
	}
	// Search the captured responses even if the pipe reports uncertain input.
	// An observed response resolves acceptance without resending the request.
	writeErr := c.pipe.Write(ctx, harnesspipe.WriteRequest{RunID: r.Thread.RunID, ID: id, Data: string(raw) + "\n"})
	var after int64
	for {
		frames, err := c.pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: r.Thread.ID, After: after})
		if err != nil {
			return nil, err
		}
		for _, f := range frames {
			after = f.Sequence
			if f.RunID != r.Thread.RunID || f.Stream != "stdout" {
				continue
			}
			if result, matched, err := providerResponse(r.Thread.Provider, id, f.Data); matched {
				return result, err
			}
		}
		if writeErr != nil && len(frames) == 0 {
			return nil, writeErr
		}
		ps, err := c.pipe.Processes(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			if p.Spec.RunID == r.Thread.RunID && p.State != "running" {
				return nil, fmt.Errorf("provider exited before %s completed", method)
			}
		}
		if len(frames) > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func providerRequest(provider, id, method string, params any) ([]byte, error) {
	request := map[string]any{"id": id, "method": method, "params": params}
	if provider == "claude" {
		body := map[string]any{"subtype": method}
		if fields, ok := params.(map[string]any); ok {
			for k, v := range fields {
				body[k] = v
			}
		}
		request = map[string]any{"type": "control_request", "request_id": id, "request": body}
	}
	return json.Marshal(request)
}

// Provider envelopes differ; the RPC waiter only deals with matched replies.
func providerResponse(provider, id string, raw json.RawMessage) (json.RawMessage, bool, error) {
	if provider == "claude" {
		var msg struct {
			Type     string `json:"type"`
			Response struct {
				ID      string          `json:"request_id"`
				Subtype string          `json:"subtype"`
				Body    json.RawMessage `json:"response"`
				Error   string          `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(raw, &msg) != nil || msg.Type != "control_response" || msg.Response.ID != id {
			return nil, false, nil
		}
		switch msg.Response.Subtype {
		case "error":
			return nil, true, &providerRPCError{msg.Response.Error}
		case "success":
			if len(msg.Response.Body) == 0 {
				return json.RawMessage(`{}`), true, nil
			}
			return msg.Response.Body, true, nil
		default:
			return nil, false, nil
		}
	}
	var msg struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.ID != id {
		return nil, false, nil
	}
	if msg.Error != nil {
		return nil, true, &providerRPCError{msg.Error.Message}
	}
	return msg.Result, msg.Result != nil, nil
}
