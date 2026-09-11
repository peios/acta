package hyperharness

import (
	"acta/internal/threads"
	"context"
)

// Save the descriptor and command receipt together. Discovery publishes only
// durable names, and replay of a completed command cannot undo a newer rename.
func (c *Controller) executeRename(ctx context.Context, r *localThread) {
	name := r.Requests[r.CommandID].Name
	var syncErr error
	pending := true
	if r.Thread.State == "running" {
		syncErr = c.renameNative(ctx, r, name, r.CommandID)
		pending = syncErr != nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return
	}
	current := c.records[r.Thread.ID]
	current.Thread.Name = name
	current.Thread.NameSyncPending = pending
	current.Thread.NameSyncError = ""
	result := threads.Result{ID: r.CommandID, ThreadID: r.Thread.ID}
	if syncErr != nil {
		current.Thread.NameSyncError = "Name saved; native title could not be confirmed: " + syncErr.Error()
		result.Error = current.Thread.NameSyncError
	}
	current.Thread.Revision++
	current.Action = ""
	current.Commands[r.CommandID] = result
	_ = c.save(current)
}

func (c *Controller) renameNative(ctx context.Context, r *localThread, name, command string) error {
	method := "thread/name/set"
	params := map[string]any{"threadId": r.Thread.ProviderID, "name": name}
	if r.Thread.Provider == "claude" {
		method = "rename_session"
		params = map[string]any{"title": name}
	}
	_, err := c.rpcID(ctx, r, method, method+"/"+command, params)
	return err
}

// Title restoration is best effort: its failure must never kill an otherwise
// healthy resumed session. Preserve a visible pending state for a later retry.
func (c *Controller) restoreNativeName(ctx context.Context, r *localThread) {
	err := c.renameNative(ctx, r, r.Thread.Name, "resume")
	r.Thread.NameSyncPending = err != nil
	r.Thread.NameSyncError = ""
	if err != nil {
		r.Thread.NameSyncError = "Native title could not be confirmed: " + err.Error()
	}
}
