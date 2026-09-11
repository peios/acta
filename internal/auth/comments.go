package auth

import (
	"acta/internal/accounts"
	"acta/internal/activity"
	"acta/internal/comments"
	"acta/internal/tasks"
	ws "acta/internal/workspaces"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func commentAbilities(v *activity.Entry, actor string, permissions []string) {
	if v.Comment == nil {
		return
	}
	allowed := v.Actor.ID == actor && ws.Contains(permissions, ws.CommentTasks) || v.Actor.ID != actor && ws.Contains(permissions, ws.ManageComments)
	v.Comment.CanEdit = allowed && !v.Comment.Deleted
	v.Comment.CanDelete = allowed && !v.Comment.Deleted
}
func (m *Management) commentScope(ctx context.Context, token, ref string, write bool, fn func(WorkspaceTx, tasks.Task, ws.Access) error) error {
	return m.workspaceTx(ctx, token, write, func(tx WorkspaceTx) error {
		t, e := tx.TaskGet(ctx, ref)
		if e != nil {
			return e
		}
		_, a, e := workspaceAccess(ctx, tx, t.WorkspaceID, false)
		if e != nil {
			return e
		}
		return fn(tx, t, a)
	})
}
func (m *Management) Comment(ctx context.Context, token, ref, id string) (activity.Entry, error) {
	var v activity.Entry
	e := m.commentScope(ctx, token, ref, false, func(tx WorkspaceTx, t tasks.Task, a ws.Access) error {
		var e error
		v, e = tx.CommentGet(ctx, t.ID, id)
		if e == nil {
			commentAbilities(&v, tx.Actor().ID, commentPermissions(t, a.Permissions))
		}
		return e
	})
	if e != nil {
		return activity.Entry{}, e
	}
	return v, nil
}
func (m *Management) CommentReplies(ctx context.Context, token, ref, id, cursor string) (activity.Page, error) {
	var v activity.Page
	before, e := activity.Cursor(cursor)
	if e != nil {
		return v, e
	}
	e = m.commentScope(ctx, token, ref, false, func(tx WorkspaceTx, t tasks.Task, a ws.Access) error {
		root, e := tx.CommentGet(ctx, t.ID, id)
		if e != nil {
			return e
		}
		if root.Comment.ThreadID != "" {
			return &accounts.FieldError{Field: "comment", Message: "Use the root comment UUID to read a thread."}
		}
		v, e = tx.CommentReplies(ctx, t.ID, id, before)
		for i := range v.Entries {
			commentAbilities(&v.Entries[i], tx.Actor().ID, commentPermissions(t, a.Permissions))
		}
		return e
	})
	if e != nil {
		return activity.Page{}, e
	}
	return v, nil
}
func (m *Management) CreateComment(ctx context.Context, token, ref string, in comments.Create) (activity.Entry, error) {
	var v activity.Entry
	if e := comments.ValidateCreate(in); e != nil {
		return v, e
	}
	var e error
	in.Body, e = comments.Body(in.Body)
	if e != nil {
		return v, e
	}
	raw, _ := json.Marshal(in)
	hash := sha256.Sum256(raw)
	e = m.commentScope(ctx, token, ref, true, func(tx WorkspaceTx, t tasks.Task, a ws.Access) error {
		if t.Archived {
			return tasks.ArchivedError()
		}
		if !ws.Contains(a.Permissions, ws.CommentTasks) {
			return ErrForbidden
		}
		var e error
		in.Body, e = tx.TaskDescription(ctx, t.WorkspaceID, in.Body)
		if e != nil {
			return e
		}
		v, e = tx.CommentCreate(ctx, t, in, hex.EncodeToString(hash[:]))
		if e == nil {
			commentAbilities(&v, tx.Actor().ID, commentPermissions(t, a.Permissions))
		}
		return e
	})
	if e != nil {
		return activity.Entry{}, e
	}
	return v, nil
}
func (m *Management) UpdateComment(ctx context.Context, token, ref, id string, in comments.Update) (activity.Entry, error) {
	var v activity.Entry
	if in.Version < 1 {
		return v, &accounts.FieldError{Field: "version", Message: "Supply the comment’s current version."}
	}
	if !in.Delete {
		var e error
		in.Body, e = comments.Body(in.Body)
		if e != nil {
			return v, e
		}
	}
	e := m.commentScope(ctx, token, ref, true, func(tx WorkspaceTx, t tasks.Task, a ws.Access) error {
		if t.Archived {
			return tasks.ArchivedError()
		}
		old, e := tx.CommentGet(ctx, t.ID, id)
		if e != nil {
			return e
		}
		own := old.Actor.ID == tx.Actor().ID
		if own && !ws.Contains(a.Permissions, ws.CommentTasks) || !own && !ws.Contains(a.Permissions, ws.ManageComments) {
			return ErrForbidden
		}
		if !in.Delete {
			in.Body, e = tx.TaskDescription(ctx, t.WorkspaceID, in.Body)
			if e != nil {
				return e
			}
		}
		v, e = tx.CommentUpdate(ctx, t, old, in)
		if e == nil {
			commentAbilities(&v, tx.Actor().ID, commentPermissions(t, a.Permissions))
		}
		return e
	})
	if e != nil {
		return activity.Entry{}, e
	}
	return v, nil
}
