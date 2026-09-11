package postgres

import (
	"acta/internal/migration"
	"acta/internal/tasks"
	ws "acta/internal/workspaces"
	"context"
	"github.com/google/uuid"
	"strings"
	"time"
)

func (t workspaceTx) migrationWorkspace(ctx context.Context, op string, in migration.Request, now time.Time) (string, error) {
	f := migrationFields(in.Fields)
	w := ws.Workspace{ID: uuid.NewString(), CreatedAt: now}
	var e error
	if op == "edit" {
		w, e = t.Lookup(ctx, in.ID, false)
		if e != nil {
			return "", e
		}
	}
	for k, p := range map[string]*string{"name": &w.Name, "slug": &w.Slug, "description": &w.Description} {
		if *p, e = f.text(k, *p); e != nil {
			return "", e
		}
	}
	w.Name, w.Slug, w.Description, e = ws.Profile(w.Name, w.Slug, w.Description)
	if e != nil {
		return "", e
	}
	w.CreatedAt, e = f.stamp("created_at", w.CreatedAt)
	if e != nil {
		return "", e
	}
	if op == "create" {
		creator := t.actor.ID
		if t.actor.ParentID != nil {
			creator = *t.actor.ParentID
		}
		e = t.Create(ctx, w, creator)
	} else {
		e = t.Profile(ctx, w)
	}
	if e != nil {
		return "", e
	}
	if _, e = t.tx.Exec(ctx, `UPDATE workspaces SET created_at=$2,version=version+1 WHERE id=$1`, w.ID, w.CreatedAt); e != nil {
		return "", e
	}
	c, e := t.TaskConfig(ctx, w.ID)
	if e != nil {
		return "", e
	}
	prefix, e := f.text("prefix", c.Prefix)
	if e != nil {
		return "", e
	}
	prefix, e = tasks.Prefix(prefix)
	if e != nil {
		return "", e
	}
	if prefix != c.Prefix {
		if e = t.TaskPrefix(ctx, w.ID, prefix, c.Version); e != nil {
			return "", e
		}
		c, e = t.TaskConfig(ctx, w.ID)
		if e != nil {
			return "", e
		}
	}
	board, e := f.text("status_board", "tasks")
	if e != nil {
		return "", e
	}
	board, e = tasks.BoardSlug(board)
	if e != nil {
		return "", e
	}
	var boardConfig tasks.Board
	for _, b := range c.Boards {
		if b.Slug == board {
			boardConfig = b
		}
	}
	_, set := f["statuses"]
	_, setCreation := f["creation_status"]
	_, setCompleted := f["completed_status"]
	if set || setCreation || setCompleted {
		names := []string{}
		start, end := "", ""
		old := map[string]string{}
		for _, s := range c.Statuses {
			if s.Board != board {
				continue
			}
			names = append(names, s.Name)
			old[strings.ToLower(s.Name)] = s.ID
			if s.ID == boardConfig.Creation {
				start = s.Name
			}
			if s.ID == boardConfig.Completed {
				end = s.Name
			}
		}
		names, e = f.list("statuses", names)
		if e != nil {
			return "", e
		}
		start, e = f.text("creation_status", start)
		if e != nil {
			return "", e
		}
		end, e = f.text("completed_status", end)
		if e != nil {
			return "", e
		}
		change := tasks.StatusChange{Board: board, Version: c.Version, Statuses: []tasks.Status{}, Replacements: map[string]string{}}
		for _, name := range names {
			id := old[strings.ToLower(name)]
			if id == "" {
				id = uuid.NewString()
			}
			change.Statuses = append(change.Statuses, tasks.Status{ID: id, Name: name})
			if strings.EqualFold(name, start) {
				change.Creation = id
			}
			if strings.EqualFold(name, end) {
				change.Completed = id
			}
		}
		if e = tasks.ValidateStatuses(change); e != nil {
			return "", e
		}
		for _, previous := range c.Statuses {
			if previous.Board != board {
				continue
			}
			kept := false
			for _, next := range change.Statuses {
				kept = kept || next.ID == previous.ID
			}
			if !kept {
				var used bool
				if e = t.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE workspace_id=$1 AND status_id=$2)`, w.ID, previous.ID).Scan(&used); e != nil {
					return "", e
				}
				if used {
					return "", migration.Invalid("statuses", "A status used by tasks cannot be removed during migration.")
				}
				change.Replacements[previous.ID] = change.Creation
			}
		}
		if e = t.TaskStatuses(ctx, w.ID, change); e != nil {
			return "", e
		}
	}
	return w.ID, nil
}
