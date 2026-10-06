package auth

import (
	"acta/internal/accounts"
	"acta/internal/tasks"
	ws "acta/internal/workspaces"
	"context"
	"errors"
)

func (m *Management) TaskReleases(ctx context.Context, token, w string) ([]tasks.Release, error) {
	var out []tasks.Release
	e := m.taskScope(ctx, token, w, false, "", func(tx WorkspaceTx, w string) error { var e error; out, e = tx.TaskReleases(ctx, w); return e })
	return out, e
}
func (m *Management) TaskRelease(ctx context.Context, token, w, id string) (tasks.Release, error) {
	var out tasks.Release
	e := m.taskScope(ctx, token, w, false, "", func(tx WorkspaceTx, w string) error { var e error; out, e = tx.TaskRelease(ctx, w, id); return e })
	return out, e
}
func (m *Management) CreateTaskRelease(ctx context.Context, token, w string, in tasks.ReleaseCreate) (tasks.Release, error) {
	var out tasks.Release
	in, e := tasks.NormalizeReleaseCreate(in)
	if e != nil {
		return out, e
	}
	e = m.taskScope(ctx, token, w, true, ws.ManageReleases, func(tx WorkspaceTx, w string) error {
		var e error
		if in.Description, e = tx.TaskDescription(ctx, w, in.Description); e != nil {
			return e
		}
		out, e = tx.TaskReleaseCreate(ctx, w, in)
		return e
	})
	return out, e
}
func (m *Management) UpdateTaskRelease(ctx context.Context, token, w, id string, in tasks.ReleaseUpdate) (tasks.Release, error) {
	var out tasks.Release
	e := m.taskScope(ctx, token, w, true, ws.ManageReleases, func(tx WorkspaceTx, w string) error {
		current, e := tx.TaskRelease(ctx, w, id)
		if e != nil {
			return e
		}
		next, e := in.Apply(current)
		if e != nil {
			return e
		}
		if next == current {
			out = current
			return nil
		}
		if current.Version != in.Version {
			return tasks.ErrReleaseConflict
		}
		if in.Description != nil {
			if next.Description, e = tx.TaskDescription(ctx, w, next.Description); e != nil {
				return e
			}
		}
		out, e = tx.TaskReleaseSave(ctx, next, in.Version)
		return e
	})
	return out, e
}

// taskReleaseID resolves an optional target release inside the task's workspace.
func taskReleaseID(ctx context.Context, tx WorkspaceTx, w, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	r, e := tx.TaskRelease(ctx, w, id)
	if errors.Is(e, ErrNotFound) {
		return "", &accounts.FieldError{Field: "release_id", Message: "Choose a release in this workspace."}
	}
	return r.ID, e
}
func releaseGroups(rs []tasks.Release) []tasks.Group {
	out := []tasks.Group{{ID: tasks.NoRelease, Name: "No release", Available: true}}
	for _, r := range rs {
		out = append(out, tasks.Group{ID: r.ID, Name: r.Name, Available: true})
	}
	return out
}
