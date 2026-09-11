package hyperharness

import (
	"acta/internal/harnesspipe"
	"acta/internal/threadadapter"
	"acta/internal/threads"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

func permissionParams(native, mode string) map[string]any {
	policy, reviewer, sandbox := "on-request", "user", "workspaceWrite"
	if mode == "automatic" {
		reviewer = "auto_review"
	}
	if mode == "bypass" {
		policy = "never"
		sandbox = "dangerFullAccess"
	}
	return map[string]any{"threadId": native, "approvalPolicy": policy, "approvalsReviewer": reviewer, "sandboxPolicy": map[string]any{"type": sandbox}}
}
func claudeMode(mode string) string {
	switch mode {
	case "ask":
		return "default"
	case "automatic":
		return "auto"
	case "bypass":
		return "bypassPermissions"
	}
	return ""
}
func (c *Controller) applyPermissions(ctx context.Context, r *localThread, id, mode string) error {
	if r.Thread.Provider == "claude" {
		processes, e := c.pipe.Processes(ctx)
		if e != nil {
			return e
		}
		for _, process := range processes {
			if process.Spec.RunID == r.Thread.RunID {
				i := slices.Index(process.Spec.Args, "--permission-prompt-tool")
				if i < 0 || i+1 >= len(process.Spec.Args) || process.Spec.Args[i+1] != "stdio" {
					return &providerRPCError{"Resume this Claude thread once to enable interactive approval requests."}
				}
			}
		}
		raw, err := c.rpcID(ctx, r, "set_permission_mode", "set_permission_mode/"+id, map[string]any{"mode": claudeMode(mode)})
		if err != nil {
			return err
		}
		var reply struct {
			Mode string `json:"mode"`
		}
		_ = json.Unmarshal(raw, &reply)
		// Some hosts acknowledge without a mode. Read the effective mode back.
		raw, err = c.rpcID(ctx, r, "initialize", "initialize/permissions/"+id, map[string]any{})
		if err != nil {
			return err
		}
		var init struct {
			Mode string `json:"current_permission_mode"`
		}
		if json.Unmarshal(raw, &init) != nil || init.Mode != claudeMode(mode) {
			return errors.New("Claude did not confirm the requested permission mode")
		}
		return nil
	}
	_, err := c.rpcID(ctx, r, "thread/settings/update", "thread/settings/update/permissions-"+id, permissionParams(r.Thread.ProviderID, mode))
	if err != nil {
		return err
	}
	// A transport acknowledgement does not establish the effective policy.
	// Use the latest effective configuration from this run. Codex emits no
	// settings notification for a no-op update; its correlated start/resume
	// response already confirms the unchanged policy. A bare ACK is insufficient.
	var after int64
	matched := false
	for {
		frames, e := c.pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: r.Thread.ID, After: after})
		if e != nil {
			return e
		}
		for _, f := range frames {
			after = f.Sequence
			if f.RunID != r.Thread.RunID || f.Stream != "stdout" {
				continue
			}
			if settings, ok := codexPermissionObservation(f.Data, r.Thread.RunID, r.Thread.ProviderID); ok {
				want := permissionParams(r.Thread.ProviderID, mode)
				matched = settings.Policy == want["approvalPolicy"] && settings.Reviewer == want["approvalsReviewer"] && settings.Sandbox.Type == want["sandboxPolicy"].(map[string]any)["type"]
			}
		}
		if len(frames) > 0 {
			continue
		}
		if matched {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("Provider has not confirmed the permission configuration")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Both provider configuration shapes describe effective settings. Keep strict
// run/request and native-thread identity checks so older sessions cannot confirm
// a new run, and later observations replace earlier ones even when mismatched.
type codexPermissionSettings struct {
	Policy   string `json:"approvalPolicy"`
	Reviewer string `json:"approvalsReviewer"`
	Sandbox  struct {
		Type string `json:"type"`
	} `json:"sandboxPolicy"`
}

func codexPermissionObservation(raw json.RawMessage, run, native string) (codexPermissionSettings, bool) {
	var m struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			ThreadID string                  `json:"threadId"`
			Settings codexPermissionSettings `json:"threadSettings"`
		} `json:"params"`
		Result struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
			Policy   string `json:"approvalPolicy"`
			Reviewer string `json:"approvalsReviewer"`
			Sandbox  struct {
				Type string `json:"type"`
			} `json:"sandbox"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return codexPermissionSettings{}, false
	}
	if m.Method == "thread/settings/updated" && m.Params.ThreadID == native {
		return m.Params.Settings, true
	}
	if (m.ID == run+"/thread/resume" || m.ID == run+"/thread/start") && m.Result.Thread.ID == native {
		settings := codexPermissionSettings{Policy: m.Result.Policy, Reviewer: m.Result.Reviewer}
		settings.Sandbox.Type = m.Result.Sandbox.Type
		return settings, true
	}
	return codexPermissionSettings{}, false
}

func (c *Controller) executePermissions(ctx context.Context, r *localThread) {
	q := r.Requests[r.CommandID]
	result := threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected"}
	if !r.SendAttempted {
		// Both providers support live permission controls. Keep the durable
		// write and effective-policy confirmation, but do not require an idle
		// turn or answer any outstanding approval on the user's behalf.
		if !c.prepareInput(r) {
			return
		}
	}
	err := c.applyPermissions(ctx, r, q.ID, q.PermissionMode)
	if err != nil {
		result.Outcome = "uncertain"
		var rejected *providerRPCError
		if errors.As(err, &rejected) {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
	} else {
		result.Outcome = "accepted"
		result.PermissionMode = q.PermissionMode
	}
	c.finishOperation(r, result)
}

// Read the pipe's authoritative capture journal, not a browser-supplied tool
// body. Recheck resolution and turn completion before releasing a decision.
func (c *Controller) pendingApproval(ctx context.Context, r *localThread, id string) (*threadadapter.Approval, error) {
	var found *threadadapter.Approval
	foundNative, foundLane := "", ""
	adapter := r.Adapter
	adapter.NativeID = r.Thread.ProviderID
	var after int64
	for {
		frames, err := c.pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: r.Thread.ID, After: after})
		if err != nil {
			return nil, err
		}
		if len(frames) == 0 {
			break
		}
		for _, f := range frames {
			after = f.Sequence
			if f.RunID != r.Thread.RunID || f.Stream != "stdout" {
				continue
			}
			native, lane, known := adapter.RequestLane(r.Thread.Provider, r.Thread.RunID, f.Data)
			if !known {
				continue
			}
			if a := threadadapter.ParseApproval(r.Thread.Provider, r.Thread.RunID, native, f.Data); a != nil && a.ID == id {
				found = a
				foundNative, foundLane = native, lane
			}
			if threadadapter.ApprovalResolved(r.Thread.Provider, r.Thread.RunID, native, f.Data) == id {
				found = nil
			}
			if found != nil {
				var m map[string]json.RawMessage
				_ = json.Unmarshal(f.Data, &m)
				var typ, method string
				_ = json.Unmarshal(m["type"], &typ)
				_ = json.Unmarshal(m["method"], &method)
				if r.Thread.Provider == "claude" && typ == "result" && lane == foundLane {
					found = nil
				}
				if method == "turn/completed" {
					var p struct {
						ThreadID string `json:"threadId"`
						Turn     struct {
							ID string `json:"id"`
						} `json:"turn"`
					}
					_ = json.Unmarshal(m["params"], &p)
					if p.ThreadID == foundNative && found.Display["turn_id"] == p.Turn.ID && found.Display["blocking"] != false {
						found = nil
					}
				}
			}
		}
	}
	if found == nil {
		return nil, errors.New("This request is no longer pending in the current provider run.")
	}
	return found, nil
}
func (c *Controller) executeApproval(ctx context.Context, r *localThread) {
	q := r.Requests[r.CommandID]
	result := threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected", Decision: q.Decision, Answers: q.Answers}
	requestID := q.ApprovalID
	if q.Action == "answer" {
		requestID = q.QuestionID
		result.Decision = "answer"
	}
	var err error
	raw := r.ApprovalReply
	if !r.SendAttempted {
		a, e := c.pendingApproval(ctx, r, requestID)
		if e != nil {
			result.Error = e.Error()
			c.finishOperation(r, result)
			return
		}
		var encoded []byte
		if q.Action == "answer" {
			encoded, e = a.Answer(q.Answers)
		} else {
			encoded, e = a.Reply(q.Decision == "approve")
		}
		if e != nil {
			result.Error = e.Error()
			c.finishOperation(r, result)
			return
		}
		raw = string(encoded)
		c.mu.Lock()
		current := c.records[r.Thread.ID]
		current.ApprovalReply = raw
		current.SendAttempted = true
		err = c.save(current)
		c.mu.Unlock()
		if err != nil {
			return
		}
	}
	if raw == "" {
		result.Error = "Saved approval response is missing"
		c.finishOperation(r, result)
		return
	}
	err = c.pipe.Write(ctx, harnesspipe.WriteRequest{RunID: r.Thread.RunID, ID: r.Thread.RunID + "/approval/" + requestID, Data: raw + "\n"})
	if err != nil {
		result.Outcome = "uncertain"
		result.Error = err.Error()
	} else {
		result.Outcome = "accepted"
	}
	c.finishOperation(r, result)
}
