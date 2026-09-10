package auth

import (
	"acta2/internal/accounts"
	"acta2/internal/activity"
	"acta2/internal/comments"
	"acta2/internal/tasks"
	ws "acta2/internal/workspaces"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
)

// TaskTx is implemented by the same consistent transaction as workspace access.
type TaskTx interface {
	TaskArchive(context.Context, tasks.Task, tasks.Archive) (tasks.Task, error)
	SearchTasks(context.Context, []string, tasks.SearchQuery, tasks.SearchPosition) (tasks.SearchPage, error)
	TaskFollowing(context.Context, string) (bool, error)
	SetTaskFollowing(context.Context, string, bool) error
	CommentGet(context.Context, string, string) (activity.Entry, error)
	CommentReplies(context.Context, string, string, int64) (activity.Page, error)
	CommentCreate(context.Context, tasks.Task, comments.Create, string) (activity.Entry, error)
	CommentUpdate(context.Context, tasks.Task, activity.Entry, comments.Update) (activity.Entry, error)

	TaskActivity(context.Context, string, int64) (activity.Page, error)
	ReadTaskActivity(context.Context, string, []activity.Seen) error
	TaskViews(context.Context, string, ...string) ([]tasks.View, error)
	CreateTaskView(context.Context, string, string, tasks.ViewSettings) (tasks.View, error)
	SaveTaskView(context.Context, string, string, int64, tasks.ViewSettings) (tasks.View, error)
	RenameTaskView(context.Context, string, string, int64, string) (tasks.View, error)
	DeleteTaskView(context.Context, string, string, int64) error
	TaskConfig(context.Context, string) (tasks.Config, error)
	TaskPrefix(context.Context, string, string, int64) error
	TaskStatuses(context.Context, string, tasks.StatusChange) error
	TaskGet(context.Context, string) (tasks.Task, error)
	TaskList(context.Context, string, tasks.Filter) (tasks.Page, error)
	TaskCreate(context.Context, string, tasks.Create) (tasks.Task, error)
	TaskPatch(context.Context, tasks.Task, tasks.Patch) (tasks.Task, error)
	TaskGroupPeople(context.Context, string) ([]tasks.Person, error)
	TaskPeople(context.Context, string, string) ([]tasks.Person, error)
	TaskDescription(context.Context, string, string) (string, error)
}

func (m *Management) taskScope(ctx context.Context, token, id string, write bool, permission string, fn func(WorkspaceTx, string) error) error {
	return m.workspaceTx(ctx, token, write, func(tx WorkspaceTx) error {
		_, parseError := uuid.Parse(id)
		w, a, e := workspaceAccess(ctx, tx, strings.ToLower(id), parseError != nil)
		if e != nil {
			return e
		}
		if permission != "" && !ws.Contains(a.Permissions, permission) {
			return ErrForbidden
		}
		return fn(tx, w.ID)
	})
}
func (m *Management) TaskConfig(ctx context.Context, token, id string) (tasks.Config, error) {
	var c tasks.Config
	e := m.taskScope(ctx, token, id, false, "", func(tx WorkspaceTx, id string) error { var e error; c, e = tx.TaskConfig(ctx, id); return e })
	return c, e
}
func (m *Management) TaskPrefix(ctx context.Context, token, id, prefix string, version int64) error {
	p, e := tasks.Prefix(prefix)
	if e != nil {
		return e
	}
	return m.taskScope(ctx, token, id, true, ws.Edit, func(tx WorkspaceTx, id string) error { return tx.TaskPrefix(ctx, id, p, version) })
}
func (m *Management) TaskStatuses(ctx context.Context, token, id string, c tasks.StatusChange) error {
	if e := tasks.ValidateStatuses(c); e != nil {
		return e
	}
	for _, s := range c.Statuses {
		if _, e := uuid.Parse(s.ID); e != nil {
			return &accounts.FieldError{Field: "statuses", Message: "Each status needs a UUID."}
		}
	}
	return m.taskScope(ctx, token, id, true, ws.ManageStatuses, func(tx WorkspaceTx, id string) error { return tx.TaskStatuses(ctx, id, c) })
}
func (m *Management) Task(ctx context.Context, token, ref string) (tasks.Task, error) {
	var out tasks.Task
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		var e error
		out, e = tx.TaskGet(ctx, ref)
		if e != nil {
			return e
		}
		_, _, e = workspaceAccess(ctx, tx, out.WorkspaceID, false)
		return e
	})
	if e != nil {
		return tasks.Task{}, e
	}
	return out, e
}
func (m *Management) Tasks(ctx context.Context, token, id string, f tasks.Filter) (tasks.Page, error) {
	var out tasks.Page
	var e error
	f, e = tasks.NormalizeFilter(f)
	if e != nil {
		return out, e
	}
	e = m.taskScope(ctx, token, id, false, "", func(tx WorkspaceTx, id string) error { var e error; out, e = tx.TaskList(ctx, id, f); return e })
	return out, e
}
func taskRelation(ctx context.Context, tx WorkspaceTx, w, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	p, e := tx.TaskGet(ctx, ref)
	if e != nil {
		return "", e
	}
	if p.WorkspaceID != w {
		return "", ErrNotFound
	}
	if p.Archived {
		return "", tasks.ArchivedError()
	}
	return p.ID, nil
}
func validAssignees(ctx context.Context, tx WorkspaceTx, w string, ids, before []string) error {
	if len(ids) > 100 {
		return &accounts.FieldError{Field: "assignees", Message: "Choose at most 100 assignees."}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return &accounts.FieldError{Field: "assignees", Message: "Choose distinct assignees."}
		}
		seen[id] = true
		if ws.Contains(before, id) {
			continue
		}
		a, e := tx.Account(ctx, id)
		if e != nil {
			return ErrNotFound
		}
		access, e := tx.Access(ctx, a, w)
		if e != nil {
			return e
		}
		if !a.Available() || !access.Allowed {
			return &accounts.FieldError{Field: "assignees", Message: "New assignees must have access to this workspace."}
		}
	}
	return nil
}
func taskStatus(ctx context.Context, tx WorkspaceTx, workspace, id string) error {
	c, e := tx.TaskConfig(ctx, workspace)
	if e != nil {
		return e
	}
	for _, s := range c.Statuses {
		if s.ID == id {
			return nil
		}
	}
	return &accounts.FieldError{Field: "status_id", Message: "Choose a status in this workspace."}
}
func (m *Management) CreateTask(ctx context.Context, token, w string, in tasks.Create) (tasks.Task, error) {
	var out tasks.Task
	var e error
	in.Title, e = tasks.Title(in.Title)
	if e != nil {
		return out, e
	}
	if e = tasks.Description(in.Description); e != nil {
		return out, e
	}
	for name, value := range map[string]*string{"priority": &in.Priority, "type": &in.Type, "size": &in.Size} {
		*value, e = tasks.PropertyValue(name, *value)
		if e != nil {
			return out, e
		}
	}
	e = m.taskScope(ctx, token, w, true, ws.CreateTasks, func(tx WorkspaceTx, w string) error {
		var e error
		if in.StatusID != "" {
			if e = taskStatus(ctx, tx, w, in.StatusID); e != nil {
				return e
			}
		}
		in.ParentID, e = taskRelation(ctx, tx, w, in.ParentID)
		if e != nil {
			return e
		}
		if e = validAssignees(ctx, tx, w, in.Assignees, nil); e != nil {
			return e
		}
		in.Description, e = tx.TaskDescription(ctx, w, in.Description)
		if e != nil {
			return e
		}
		out, e = tx.TaskCreate(ctx, w, in)
		return e
	})
	return out, e
}
func (m *Management) PatchTask(ctx context.Context, token, ref string, in tasks.Patch) (tasks.Task, error) {
	var out tasks.Task
	e := m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		t, e := tx.TaskGet(ctx, ref)
		if e != nil {
			return e
		}
		_, access, e := workspaceAccess(ctx, tx, t.WorkspaceID, false)
		if e != nil {
			return e
		}
		if t.Archived {
			return tasks.ArchivedError()
		}
		if !ws.Contains(access.Permissions, ws.EditTasks) {
			return ErrForbidden
		}
		if in.Version < 1 {
			return &accounts.FieldError{Field: "version", Message: "Supply a positive field version from task_get."}
		}
		if len(in.Value) == 0 || strings.TrimSpace(string(in.Value)) == "null" {
			return &accounts.FieldError{Field: "value", Message: "Supply an explicit value; use an empty string or empty array to clear a field."}
		}
		switch in.Field {
		case "title", "description", "status_id", "board", "parent_id", "priority", "type", "size":
			var value string
			if e = json.Unmarshal(in.Value, &value); e != nil {
				return &accounts.FieldError{Field: in.Field, Message: "Supply a text value."}
			}
			switch in.Field {
			case "priority", "type", "size":
				value, e = tasks.PropertyValue(in.Field, value)
			case "title":
				value, e = tasks.Title(value)
			case "description":
				e = tasks.Description(value)
				if e == nil {
					value, e = tx.TaskDescription(ctx, t.WorkspaceID, value)
				}
			case "board":
				var board string
				board, e = tasks.BoardSlug(value)
				if e == nil && board == t.Board {
					out = t
					return nil
				}
				if e == nil {
					var c tasks.Config
					c, e = tx.TaskConfig(ctx, t.WorkspaceID)
					if e == nil {
						for _, b := range c.Boards {
							if b.Slug == board {
								value = b.Creation
								break
							}
						}
					}
				}
				in.Field = "status_id"
			case "status_id":
				e = taskStatus(ctx, tx, t.WorkspaceID, value)
			case "parent_id":
				value, e = taskRelation(ctx, tx, t.WorkspaceID, value)
			}
			if e != nil {
				return e
			}
			in.Value, _ = json.Marshal(value)
		case "assignees":
			var ids []string
			if e = json.Unmarshal(in.Value, &ids); e != nil {
				return &accounts.FieldError{Field: in.Field, Message: "Supply an array of account UUIDs."}
			}
			old := []string{}
			for _, p := range t.Assignees {
				old = append(old, p.ID)
			}
			if e = validAssignees(ctx, tx, t.WorkspaceID, ids, old); e != nil {
				return e
			}
		default:
			return &accounts.FieldError{Field: "field", Message: "Choose title, description, status_id, parent_id, assignees, board, priority, type or size."}
		}
		out, e = tx.TaskPatch(ctx, t, in)
		return e
	})
	return out, e
}
func (m *Management) TaskPeople(ctx context.Context, token, w, q string) ([]tasks.Person, error) {
	var out []tasks.Person
	e := m.taskScope(ctx, token, w, false, "", func(tx WorkspaceTx, w string) error {
		var e error
		out, e = tx.TaskPeople(ctx, w, strings.TrimSpace(q))
		return e
	})
	return out, e
}

func (m *Management) TaskGroups(ctx context.Context, token, w, mode string) ([]tasks.Group, error) {
	var out []tasks.Group
	if mode != "assignee" && mode != "agents" && !tasks.IsProperty(mode) {
		return out, &accounts.FieldError{Field: "group", Message: "Choose assignee, agents, priority, type or size grouping."}
	}
	e := m.taskScope(ctx, token, w, false, "", func(tx WorkspaceTx, w string) error {
		if tasks.IsProperty(mode) {
			for _, o := range tasks.PropertyOptions(mode) {
				out = append(out, tasks.Group{ID: o.Value, Name: o.Label, Available: true})
			}
			return nil
		}
		people, e := tx.TaskGroupPeople(ctx, w)
		if e != nil {
			return e
		}
		actor := tx.Actor()
		owner := actor.ID
		if actor.ParentID != nil {
			owner = *actor.ParentID
		}
		out = tasks.AssignmentGroups(mode, owner, people)
		return nil
	})
	return out, e
}
