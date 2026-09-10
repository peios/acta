package postgres

import (
	"acta2/internal/activity"
	"acta2/internal/auth"
	"acta2/internal/documents"
	"acta2/internal/tasks"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const documentColumns = `v.file_id::text,v.document_id::text,v.revision,v.title,v.filename,v.media_type,v.size,v.sha256,v.created_by::text,v.created_at`

func scanDocumentVersion(row pgx.Row) (documents.Version, error) {
	var v documents.Version
	e := row.Scan(&v.FileID, &v.DocumentID, &v.Revision, &v.Title, &v.Filename, &v.MediaType, &v.Size, &v.SHA256, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return v, e
}
func (t workspaceTx) DocumentGet(ctx context.Context, id string) (documents.Document, error) {
	var d documents.Document
	e := t.tx.QueryRow(ctx, `SELECT d.id::text,d.task_id::text,`+documentColumns+` FROM documents d JOIN document_versions v ON v.document_id=d.id AND v.revision=d.revision WHERE d.id::text=$1`, id).Scan(&d.ID, &d.TaskID, &d.FileID, &d.DocumentID, &d.Revision, &d.Title, &d.Filename, &d.MediaType, &d.Size, &d.SHA256, &d.CreatedBy, &d.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return d, e
}
func (t workspaceTx) DocumentList(ctx context.Context, task, cursor string) ([]documents.Document, error) {
	rows, e := t.tx.Query(ctx, `SELECT d.id::text,d.task_id::text,`+documentColumns+` FROM documents d JOIN document_versions v ON v.document_id=d.id AND v.revision=d.revision WHERE d.task_id=$1 AND ($2='' OR d.id::text>$2) ORDER BY d.id LIMIT 51`, task, cursor)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []documents.Document{}
	for rows.Next() {
		var d documents.Document
		e = rows.Scan(&d.ID, &d.TaskID, &d.FileID, &d.DocumentID, &d.Revision, &d.Title, &d.Filename, &d.MediaType, &d.Size, &d.SHA256, &d.CreatedBy, &d.CreatedAt)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (t workspaceTx) DocumentVersions(ctx context.Context, id string, before int64) ([]documents.Version, error) {
	rows, e := t.tx.Query(ctx, `SELECT `+documentColumns+` FROM document_versions v WHERE document_id=$1 AND ($2::bigint=0 OR revision<$2) ORDER BY revision DESC LIMIT 51`, id, before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []documents.Version{}
	for rows.Next() {
		v, e := scanDocumentVersion(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (t workspaceTx) DocumentFile(ctx context.Context, id string, revision int64) (documents.Version, []byte, error) {
	v, e := scanDocumentVersion(t.tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM document_versions v WHERE document_id=$1 AND revision=$2`, id, revision))
	if e != nil {
		return v, nil, e
	}
	var b []byte
	e = t.tx.QueryRow(ctx, `SELECT content FROM document_files WHERE file_id=$1`, v.FileID).Scan(&b)
	return v, b, e
}
func (t workspaceTx) DocumentSave(ctx context.Context, task tasks.Task, in documents.Save) (documents.Document, error) {
	var d documents.Document
	if in.Revision == 0 {
		tag, e := t.tx.Exec(ctx, `INSERT INTO documents(id,task_id,revision) VALUES($1,$2,1) ON CONFLICT(id) DO NOTHING`, in.ID, task.ID)
		if e != nil {
			return d, e
		}
		if tag.RowsAffected() != 1 {
			return d, documents.ErrConflict
		}
	} else {
		tag, e := t.tx.Exec(ctx, `UPDATE documents SET revision=revision+1 WHERE id=$1 AND task_id=$2 AND revision=$3`, in.ID, task.ID, in.Revision)
		if e != nil {
			return d, e
		}
		if tag.RowsAffected() != 1 {
			return d, documents.ErrConflict
		}
	}
	fileID := uuid.NewString()
	_, e := t.tx.Exec(ctx, `INSERT INTO document_versions(file_id,document_id,revision,title,filename,media_type,size,sha256,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, fileID, in.ID, in.Revision+1, in.Title, in.Filename, in.MediaType, len(in.Content), in.SHA256, t.actor.ID)
	if e != nil {
		return d, e
	}
	_, e = t.tx.Exec(ctx, `INSERT INTO document_files(file_id,content) VALUES($1,$2)`, fileID, in.Content)
	if e != nil {
		return d, e
	}
	kind := "document.created"
	if in.Revision > 0 {
		kind = "document.version"
	}
	e = t.recordActivity(ctx, task.WorkspaceID, "task", task.ID, activity.Change{Kind: kind, After: activity.Value{Text: fmt.Sprintf("%s · v%d", in.Title, in.Revision+1), Items: []activity.Reference{{ID: in.ID, Label: in.Title}}}})
	if e != nil {
		return d, e
	}
	if e = t.taskChanged(ctx, task.WorkspaceID); e != nil {
		return d, e
	}
	return t.DocumentGet(ctx, in.ID)
}
func (t workspaceTx) DocumentDelete(ctx context.Context, task tasks.Task, d documents.Document) error {
	tag, e := t.tx.Exec(ctx, `DELETE FROM documents WHERE id=$1 AND revision=$2`, d.ID, d.Revision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return documents.ErrConflict
	}
	e = t.recordActivity(ctx, task.WorkspaceID, "task", task.ID, activity.Change{Kind: "document.deleted", Before: activity.Value{Text: d.Title, Items: []activity.Reference{{ID: d.ID, Label: d.Title}}}})
	if e != nil {
		return e
	}
	return t.taskChanged(ctx, task.WorkspaceID)
}
