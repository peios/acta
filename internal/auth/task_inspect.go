package auth

import (
	"acta/internal/tasks"
	"context"
)

// Inspection includes a bounded direct-child page in the same authorized snapshot.
func (m *Management) InspectTask(ctx context.Context, token, ref, cursor string) (tasks.Detail, error) {
	var out tasks.Detail
	f, e := tasks.NormalizeTaskSort(tasks.Filter{State: "all", Sort: "number", Direction: "asc", Cursor: cursor})
	if e != nil {
		return out, e
	}
	e = m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		t, e := tx.TaskGet(ctx, ref)
		if e != nil {
			return e
		}
		if _, _, e = workspaceAccess(ctx, tx, t.WorkspaceID, false); e != nil {
			return e
		}
		c, e := tx.TaskConfig(ctx, t.WorkspaceID)
		if e != nil {
			return e
		}
		f.Parent = t.ID
		f.Archived = t.Archived
		children, e := tx.TaskList(ctx, t.WorkspaceID, f)
		if e != nil {
			return e
		}
		out = tasks.Detail{Task: t, Subtasks: tasks.Summarize(children, c)}
		for _, s := range c.Statuses {
			if s.ID == t.StatusID {
				out.Status = s
			}
		}
		return nil
	})
	if e != nil {
		return tasks.Detail{}, e
	}
	return out, nil
}

func (m *Management) TaskSummaries(ctx context.Context, token, workspace string, f tasks.Filter) (tasks.SummaryPage, error) {
	var out tasks.SummaryPage
	f, e := tasks.NormalizeFilter(f)
	if e != nil {
		return out, e
	}
	e = m.taskScope(ctx, token, workspace, false, "", func(tx WorkspaceTx, workspace string) error {
		p, e := tx.TaskList(ctx, workspace, f)
		if e != nil {
			return e
		}
		c, e := tx.TaskConfig(ctx, workspace)
		if e != nil {
			return e
		}
		out = tasks.Summarize(p, c)
		return nil
	})
	if e != nil {
		return tasks.SummaryPage{}, e
	}
	return out, nil
}
