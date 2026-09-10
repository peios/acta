package postgres

import (
	"acta2/internal/activity"
	"acta2/internal/tasks"
	"context"
)

func taskActivityValue(t tasks.Task, field string, c tasks.Config) activity.Value {
	v := activity.Value{}
	switch field {
	case "priority", "type", "size":
		v.Text = tasks.PropertyLabel(field, map[string]string{"priority": t.Priority, "type": t.Type, "size": t.Size}[field])
	case "title":
		v.Text = t.Title
	case "status_id":
		for _, s := range c.Statuses {
			if s.ID == t.StatusID {
				v.Items = []activity.Reference{{ID: s.ID, Label: statusActivityLabel(c, s)}}
			}
		}
	case "parent_id":
		if len(t.Ancestors) > 0 {
			p := t.Ancestors[len(t.Ancestors)-1]
			v.Items = []activity.Reference{{ID: p.ID, Label: p.Reference}}
		}
	case "assignees":
		for _, p := range t.Assignees {
			v.Items = append(v.Items, activity.Reference{ID: p.ID, Label: p.Username})
		}
	}
	return v
}
func (t workspaceTx) taskActivityChange(ctx context.Context, before, after tasks.Task, field string) error {
	c, e := t.TaskConfig(ctx, after.WorkspaceID)
	if e != nil {
		return e
	}
	return t.recordActivity(ctx, after.WorkspaceID, "task", after.ID, activity.Change{Kind: "task.changed", Field: field, Before: taskActivityValue(before, field, c), After: taskActivityValue(after, field, c)}, newMentions(before.Description, after.Description)...)
}

func statusActivityLabel(c tasks.Config, s tasks.Status) string {
	for _, b := range c.Boards {
		if b.Slug == s.Board {
			return b.Name + " / " + s.Name
		}
	}
	return s.Name
}
