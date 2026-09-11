package postgres

import (
	"acta/internal/activity"
	"acta/internal/documents"
	"acta/internal/migration"
	"context"
	"encoding/base64"
	"time"
)

func (t workspaceTx) migrationDocument(ctx context.Context, op string, in migration.Request, now time.Time) (string, error) {
	f := migrationFields(in.Fields)
	s := documents.Save{}
	var e error
	if op == "edit" {
		v, e := t.DocumentGet(ctx, in.ID)
		if e != nil {
			return "", e
		}
		s = documents.Save{ID: v.ID, Task: v.TaskID, Title: v.Title, Filename: v.Filename, Revision: v.Revision}
		if _, ok := f["content_base64"]; !ok {
			if e = t.tx.QueryRow(ctx, `SELECT content FROM document_files WHERE file_id=$1`, v.FileID).Scan(&s.Content); e != nil {
				return "", e
			}
		}
	}
	for k, p := range map[string]*string{"task_id": &s.Task, "title": &s.Title, "filename": &s.Filename} {
		if *p, e = f.text(k, *p); e != nil {
			return "", e
		}
	}
	if _, ok := f["content_base64"]; ok {
		v, e := f.text("content_base64", "")
		if e != nil {
			return "", e
		}
		s.Content, e = base64.StdEncoding.DecodeString(v)
		if e != nil {
			return "", migration.Invalid("content_base64", "Supply standard base64 file content.")
		}
	} else if op == "create" {
		return "", migration.Invalid("content_base64", "Supply file content, including empty string for an empty file.")
	}
	if e = documents.Validate(&s); e != nil {
		return "", e
	}
	task, e := t.TaskGet(ctx, s.Task)
	if e != nil {
		return "", e
	}
	at, e := f.stamp("created_at", now)
	if e != nil {
		return "", e
	}
	d, e := t.DocumentSave(ctx, task, s)
	if e != nil {
		return "", e
	}
	_, e = t.tx.Exec(ctx, `UPDATE document_versions SET created_at=$2 WHERE file_id=$1`, d.FileID, at)
	if e != nil {
		return "", e
	}
	kind := "document.created"
	if op == "edit" {
		kind = "document.updated"
	}
	_, e = t.migrationEvent(ctx, "", task.WorkspaceID, task.ID, "", t.actor.ID, activity.Change{Kind: kind, After: activity.Value{Text: d.Title, Items: []activity.Reference{{ID: d.ID, Label: d.Title}}}}, at, at)
	return d.ID, e
}
