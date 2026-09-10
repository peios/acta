package auth

import (
	"acta2/internal/accounts"
	"acta2/internal/tasks"
	ws "acta2/internal/workspaces"
	"context"
)

func commentPermissions(t tasks.Task, permissions []string) []string {
	if t.Archived {
		return nil
	}
	return permissions
}
func (m *Management) ArchiveTask(ctx context.Context, token, ref string, in tasks.Archive) (tasks.Task, error) {
	var out tasks.Task
	if in.Version < 1 {
		return out, &accounts.FieldError{Field: "version", Message: "Supply the task's current archived field version."}
	}
	err := m.commentScope(ctx, token, ref, true, func(tx WorkspaceTx, t tasks.Task, a ws.Access) error {
		if !ws.Contains(a.Permissions, ws.EditTasks) {
			return ErrForbidden
		}
		var err error
		out, err = tx.TaskArchive(ctx, t, in)
		return err
	})
	return out, err
}
