package postgres

import (
	"acta/internal/accounts"
	"acta/internal/activity"
	"acta/internal/tasks"
	"context"
	"github.com/google/uuid"
)

// Workspace mutations share the authority transaction's write lock. A subtree
// and its activity/revision change together; partial archives are never visible.
func (t workspaceTx) TaskArchive(ctx context.Context, v tasks.Task, in tasks.Archive) (tasks.Task, error) {
	if v.Archived == in.Archived {
		return v, nil
	}
	if v.Versions["archived"] != in.Version {
		return v, tasks.NewConflict(v, "archived")
	}
	if !in.Archived && v.ParentID != "" {
		parent, e := t.TaskGet(ctx, v.ParentID)
		if e != nil {
			return v, e
		}
		if parent.Archived {
			return v, &accounts.FieldError{Field: "archived", Message: "Restore this task's archived ancestors first."}
		}
	}
	tree := `WITH RECURSIVE tree AS (SELECT id FROM tasks WHERE id=$1 UNION ALL SELECT c.id FROM tasks c JOIN tree ON c.parent_id=tree.id) `
	var err error
	kind := "task.archived"
	if in.Archived {
		_, err = t.tx.Exec(ctx, tree+`UPDATE tasks SET archived_at=now(),archive_batch=$2,archive_version=archive_version+1,updated_at=now() WHERE id IN(SELECT id FROM tree) AND archived_at IS NULL`, v.ID, uuid.NewString())
	} else {
		kind = "task.restored"
		_, err = t.tx.Exec(ctx, tree+`UPDATE tasks SET archived_at=NULL,archive_batch=NULL,archive_version=archive_version+1,updated_at=now() WHERE id IN(SELECT id FROM tree) AND archive_batch=(SELECT archive_batch FROM tasks WHERE id=$1)`, v.ID)
	}
	if err != nil {
		return v, err
	}
	if err = t.taskChanged(ctx, v.WorkspaceID); err != nil {
		return v, err
	}
	if err = t.recordActivity(ctx, v.WorkspaceID, "task", v.ID, activity.Change{Kind: kind, After: activity.Value{Text: v.Title}}); err != nil {
		return v, err
	}
	return t.TaskGet(ctx, v.ID)
}
