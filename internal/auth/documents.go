package auth

import (
	"acta2/internal/documents"
	"acta2/internal/tasks"
	ws "acta2/internal/workspaces"
	"context"
	"github.com/google/uuid"
)

// All methods run inside the authorized workspace transaction. File insertion,
// latest-revision update and activity recording must succeed or roll back together.
type DocumentTx interface {
	DocumentGet(context.Context, string) (documents.Document, error)
	DocumentList(context.Context, string, string) ([]documents.Document, error)
	DocumentVersions(context.Context, string, int64) ([]documents.Version, error)
	DocumentFile(context.Context, string, int64) (documents.Version, []byte, error)
	DocumentSave(context.Context, tasks.Task, documents.Save) (documents.Document, error)
	DocumentDelete(context.Context, tasks.Task, documents.Document) error
}

func documentTask(ctx context.Context, tx WorkspaceTx, ref string, write bool) (tasks.Task, bool, error) {
	task, e := tx.TaskGet(ctx, ref)
	if e != nil {
		return task, false, e
	}
	_, a, e := workspaceAccess(ctx, tx, task.WorkspaceID, false)
	if e != nil {
		return tasks.Task{}, false, e
	}
	if write && task.Archived {
		return tasks.Task{}, false, tasks.ArchivedError()
	}
	editable := !task.Archived && ws.Contains(a.Permissions, ws.EditTasks)
	if write && !editable {
		return tasks.Task{}, false, ErrForbidden
	}
	return task, editable, nil
}
func (m *Management) Documents(ctx context.Context, token, task, cursor string) (documents.Page, error) {
	out := documents.Page{Documents: []documents.Document{}}
	if cursor != "" {
		if _, e := uuid.Parse(cursor); e != nil {
			return out, documents.Invalid("cursor", "Supply the returned cursor.")
		}
	}
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		t, write, e := documentTask(ctx, tx, task, false)
		if e != nil {
			return e
		}
		rows, e := tx.DocumentList(ctx, t.ID, cursor)
		if e != nil {
			return e
		}
		if len(rows) > documents.PageSize {
			rows = rows[:documents.PageSize]
			out.Cursor = rows[len(rows)-1].ID
		}
		for i := range rows {
			rows[i].CanWrite = write
		}
		out.Documents = rows
		return nil
	})
	return out, e
}
func documentAccess(ctx context.Context, tx WorkspaceTx, id string, write bool) (documents.Document, tasks.Task, error) {
	d, e := tx.DocumentGet(ctx, id)
	if e != nil {
		return d, tasks.Task{}, e
	}
	task, editable, e := documentTask(ctx, tx, d.TaskID, write)
	d.CanWrite = editable
	return d, task, e
}
func (m *Management) Document(ctx context.Context, token, id string) (documents.Document, error) {
	var out documents.Document
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		d, _, e := documentAccess(ctx, tx, id, false)
		if e == nil {
			out = d
		}
		return e
	})
	return out, e
}
func (m *Management) DocumentVersions(ctx context.Context, token, id string, before int64) (documents.History, error) {
	out := documents.History{Versions: []documents.Version{}}
	if before < 0 {
		return out, documents.Invalid("before", "Supply the returned before revision.")
	}
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		d, _, e := documentAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		rows, e := tx.DocumentVersions(ctx, d.ID, before)
		if e != nil {
			return e
		}
		if len(rows) > documents.PageSize {
			rows = rows[:documents.PageSize]
			out.Before = rows[len(rows)-1].Revision
		}
		out.Versions = rows
		return nil
	})
	return out, e
}
func (m *Management) DocumentFile(ctx context.Context, token, id string, revision int64) (documents.Version, []byte, error) {
	var v documents.Version
	var b []byte
	if revision < 0 {
		return v, b, documents.Invalid("revision", "Supply a positive revision, or 0 for latest.")
	}
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		d, _, e := documentAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		if revision == 0 {
			revision = d.Revision
		}
		v, b, e = tx.DocumentFile(ctx, d.ID, revision)
		return e
	})
	return v, b, e
}
func (m *Management) SaveDocument(ctx context.Context, token string, in documents.Save) (documents.Document, error) {
	var out documents.Document
	if e := documents.Validate(&in); e != nil {
		return out, e
	}
	e := m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		task, _, e := documentTask(ctx, tx, in.Task, true)
		if e != nil {
			return e
		}
		if in.Revision > 0 {
			old, _, e := documentAccess(ctx, tx, in.ID, true)
			if e != nil {
				return e
			}
			if old.TaskID != task.ID {
				return ErrNotFound
			}
			if old.Revision != in.Revision {
				return documents.ErrConflict
			}
		}
		out, e = tx.DocumentSave(ctx, task, in)
		out.CanWrite = e == nil
		return e
	})
	return out, e
}
func (m *Management) DeleteDocument(ctx context.Context, token, id string, revision int64) error {
	if revision < 1 {
		return documents.Invalid("revision", "Supply the latest document revision.")
	}
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		d, task, e := documentAccess(ctx, tx, id, true)
		if e != nil {
			return e
		}
		if d.Revision != revision {
			return documents.ErrConflict
		}
		return tx.DocumentDelete(ctx, task, d)
	})
}
