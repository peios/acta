package auth

import (
	"acta2/internal/accounts"
	"acta2/internal/activity"
	"context"
)

func (m *Management) TaskActivity(ctx context.Context, token, ref, cursor string) (activity.Page, error) {
	before, e := activity.Cursor(cursor)
	if e != nil {
		return activity.Page{}, e
	}
	var out activity.Page
	e = m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		task, e := tx.TaskGet(ctx, ref)
		if e != nil {
			return e
		}
		_, access, e := workspaceAccess(ctx, tx, task.WorkspaceID, false)
		if e != nil {
			return e
		}
		out, e = tx.TaskActivity(ctx, task.ID, before)
		for i := range out.Entries {
			commentAbilities(&out.Entries[i], tx.Actor().ID, commentPermissions(task, access.Permissions))
		}
		return e
	})
	if e != nil {
		return activity.Page{}, e
	}
	return out, nil
}
func (m *Management) ReadTaskActivity(ctx context.Context, token, ref string, seen []activity.Seen) error {
	if len(seen) < 1 || len(seen) > activity.PageSize {
		return &accounts.FieldError{Field: "entries", Message: "Acknowledge between 1 and 50 visible activity entries."}
	}
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		task, e := tx.TaskGet(ctx, ref)
		if e != nil {
			return e
		}
		_, _, e = workspaceAccess(ctx, tx, task.WorkspaceID, false)
		if e != nil {
			return e
		}
		return tx.ReadTaskActivity(ctx, task.ID, seen)
	})
}
