package auth

import (
	"acta/internal/tasks"
	"context"
	"github.com/google/uuid"
)

func (m *Management) SearchTasks(ctx context.Context, token string, q tasks.SearchQuery) (tasks.SearchPage, error) {
	out := tasks.SearchPage{Tasks: []tasks.SearchResult{}}
	q, position, e := tasks.NormalizeSearch(q)
	if e != nil {
		return out, e
	}
	e = m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		allowed := []string{}
		if q.Workspace != "" {
			_, parse := uuid.Parse(q.Workspace)
			w, _, e := workspaceAccess(ctx, tx, q.Workspace, parse != nil)
			if e != nil {
				return e
			}
			allowed = append(allowed, w.ID)
		} else {
			for offset := 0; ; {
				rows, more, e := tx.List(ctx, tx.Actor(), "", offset)
				if e != nil {
					return e
				}
				for _, w := range rows {
					a, e := tx.Access(ctx, tx.Actor(), w.ID)
					if e != nil {
						return e
					}
					if a.Allowed {
						allowed = append(allowed, w.ID)
					}
				}
				if !more {
					break
				}
				offset += len(rows)
			}
		}
		var e error
		out, e = tx.SearchTasks(ctx, allowed, q, position)
		return e
	})
	if e != nil {
		return tasks.SearchPage{}, e
	}
	return out, nil
}
