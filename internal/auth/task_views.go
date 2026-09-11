package auth

import (
	"acta/internal/tasks"
	"context"
)

// Personal views require workspace access, but never grant task-editing authority.
func (m *Management) TaskViews(ctx context.Context, token, workspace string, boards ...string) ([]tasks.View, error) {
	var views []tasks.View
	e := m.taskScope(ctx, token, workspace, true, "", func(tx WorkspaceTx, workspace string) error {
		var e error
		views, e = tx.TaskViews(ctx, workspace, boards...)
		return e
	})
	return views, e
}
func (m *Management) CreateTaskView(ctx context.Context, token, workspace, name string, settings tasks.ViewSettings) (tasks.View, error) {
	var out tasks.View
	name, e := tasks.ViewName(name)
	if e != nil {
		return out, e
	}
	settings, e = tasks.NormalizeViewSettings(settings)
	if e != nil {
		return out, e
	}
	e = m.taskScope(ctx, token, workspace, true, "", func(tx WorkspaceTx, workspace string) error {
		// Initialize the starting tab even when the first request is a create.
		if _, e := tx.TaskViews(ctx, workspace, settings.Board); e != nil {
			return e
		}
		var e error
		out, e = tx.CreateTaskView(ctx, workspace, name, settings)
		return e
	})
	return out, e
}
func (m *Management) SaveTaskView(ctx context.Context, token, workspace, id string, version int64, settings tasks.ViewSettings) (tasks.View, error) {
	var out tasks.View
	settings, e := tasks.NormalizeViewSettings(settings)
	if e != nil {
		return out, e
	}
	e = m.taskScope(ctx, token, workspace, true, "", func(tx WorkspaceTx, workspace string) error {
		var e error
		out, e = tx.SaveTaskView(ctx, workspace, id, version, settings)
		return e
	})
	return out, e
}

func (m *Management) RenameTaskView(ctx context.Context, token, workspace, id string, version int64, name string) (tasks.View, error) {
	var out tasks.View
	name, e := tasks.ViewName(name)
	if e != nil {
		return out, e
	}
	e = m.taskScope(ctx, token, workspace, true, "", func(tx WorkspaceTx, workspace string) error {
		var e error
		out, e = tx.RenameTaskView(ctx, workspace, id, version, name)
		return e
	})
	return out, e
}
func (m *Management) DeleteTaskView(ctx context.Context, token, workspace, id string, version int64) error {
	return m.taskScope(ctx, token, workspace, true, "", func(tx WorkspaceTx, workspace string) error {
		return tx.DeleteTaskView(ctx, workspace, id, version)
	})
}
