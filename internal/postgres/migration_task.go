package postgres

import (
	"acta2/internal/activity"
	"acta2/internal/migration"
	"acta2/internal/tasks"
	"context"
	"github.com/google/uuid"
	"math"
	"strconv"
	"strings"
	"time"
)

func (t workspaceTx) migrationTask(ctx context.Context, op string, in migration.Request, now time.Time) (string, error) {
	f := migrationFields(in.Fields)
	v := tasks.Task{CreatedAt: now, Assignees: []tasks.Person{}}
	creator := t.actor.ID
	var e error
	if op == "edit" {
		v, e = t.TaskGet(ctx, in.ID)
		if e != nil {
			return "", e
		}
		if e = t.tx.QueryRow(ctx, `SELECT created_by::text FROM tasks WHERE id=$1`, v.ID).Scan(&creator); e != nil {
			return "", e
		}
	}
	for k, p := range map[string]*string{"workspace_id": &v.WorkspaceID, "title": &v.Title, "description": &v.Description, "priority": &v.Priority, "type": &v.Type, "size": &v.Size, "status_id": &v.StatusID} {
		if *p, e = f.text(k, *p); e != nil {
			return "", e
		}
	}
	if v.Title, e = tasks.Title(v.Title); e != nil {
		return "", e
	}
	if e = tasks.Description(v.Description); e != nil {
		return "", e
	}
	for k, p := range map[string]*string{"priority": &v.Priority, "type": &v.Type, "size": &v.Size} {
		if *p, e = tasks.PropertyValue(k, *p); e != nil {
			return "", e
		}
	}
	c, e := t.TaskConfig(ctx, v.WorkspaceID)
	if e != nil {
		return "", migration.Invalid("workspace_id", "Choose an existing workspace UUID.")
	}
	if v.StatusID == "" {
		v.StatusID = c.Creation
	}
	valid := false
	for _, s := range c.Statuses {
		valid = valid || s.ID == v.StatusID
	}
	if !valid {
		return "", migration.Invalid("status_id", "Choose a status in this workspace.")
	}
	v.ParentID, e = f.nullable("parent_id", v.ParentID)
	if e != nil {
		return "", e
	}
	if v.ParentID != "" {
		p, e := t.TaskGet(ctx, v.ParentID)
		if e != nil {
			return "", e
		}
		if p.WorkspaceID != v.WorkspaceID {
			return "", migration.Invalid("parent_id", "Parent must belong to the same workspace.")
		}
		v.ParentID = p.ID
		for _, ancestor := range append(p.Ancestors, tasks.Source{ID: p.ID}) {
			if ancestor.ID == v.ID {
				return "", migration.Invalid("parent_id", "A task cannot be its own ancestor.")
			}
		}
	}
	ids := []string{}
	for _, a := range v.Assignees {
		ids = append(ids, a.ID)
	}
	ids, e = f.list("assignees", ids)
	if e != nil {
		return "", e
	}
	if len(ids) > 100 {
		return "", migration.Invalid("assignees", "At most 100 assignments.")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return "", migration.Invalid("assignees", "Duplicate assignment.")
		}
		seen[id] = true
		if _, e = t.Account(ctx, id); e != nil {
			return "", e
		}
	}
	creator, e = t.migrationAuthor(ctx, f, "created_by", creator)
	if e != nil {
		return "", e
	}
	created, updated, e := f.dates(v.CreatedAt, now)
	if e != nil {
		return "", e
	}
	archived := v.ArchivedAt
	if raw, ok := f["archived_at"]; ok {
		if strings.TrimSpace(string(raw)) == "null" {
			archived = nil
		} else {
			at, e := f.stamp("archived_at", now)
			if e != nil {
				return "", e
			}
			archived = &at
		}
	}
	if archived != nil && archived.Before(created) {
		return "", migration.Invalid("archived_at", "Archive date cannot precede creation.")
	}
	ref, e := f.text("reference", "")
	if e != nil {
		return "", e
	}
	n := v.Number
	if _, supplied := f["reference"]; supplied && ref == "" {
		return "", migration.Invalid("reference", "Supply a task reference, or omit this field.")
	}
	if ref != "" {
		parts := referencePattern.FindStringSubmatch(ref)
		if parts == nil || strings.ToUpper(parts[1]) != c.Prefix {
			return "", migration.Invalid("reference", "Use this workspace's current prefix and a positive number.")
		}
		n, e = strconv.ParseInt(parts[2], 10, 64)
		if e != nil || n >= math.MaxInt64 {
			return "", migration.Invalid("reference", "Task number is too large.")
		}
		var exists bool
		e = t.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE workspace_id=$1 AND number=$2 AND id::text<>$3)`, v.WorkspaceID, n, v.ID).Scan(&exists)
		if e != nil {
			return "", e
		}
		if exists {
			return "", migration.Invalid("reference", "That task reference is already in use.")
		}
	}
	if op == "create" {
		v, e = t.TaskCreate(ctx, v.WorkspaceID, tasks.Create{Title: v.Title, Description: v.Description, Priority: v.Priority, Type: v.Type, Size: v.Size, StatusID: v.StatusID, ParentID: v.ParentID, Assignees: ids})
		if e != nil {
			return "", e
		}
		if n == 0 {
			n = v.Number
		}
	}
	var batch any
	if archived != nil {
		batch = uuid.NewString()
	}
	_, e = t.tx.Exec(ctx, `UPDATE tasks SET number=$2,title=$3,description=$4,priority=$5,type=$6,size=$7,status_id=$8,parent_id=NULLIF($9,'')::uuid,created_by=$10,created_at=$11,updated_at=$12,archived_at=$13,archive_batch=CASE WHEN $13::timestamptz IS NULL THEN NULL ELSE COALESCE(archive_batch,$14::uuid) END,title_version=title_version+1,description_version=description_version+1,priority_version=priority_version+1,type_version=type_version+1,size_version=size_version+1,status_version=status_version+1,parent_version=parent_version+1,assignees_version=assignees_version+1,archive_version=archive_version+1 WHERE id=$1`, v.ID, n, v.Title, v.Description, v.Priority, v.Type, v.Size, v.StatusID, v.ParentID, creator, created, updated, archived, batch)
	if e != nil {
		return "", e
	}
	if e = t.setTaskAssignees(ctx, v.ID, ids); e != nil {
		return "", e
	}
	_, e = t.tx.Exec(ctx, `UPDATE task_settings SET next_number=GREATEST(next_number,$2+1),revision=revision+1 WHERE workspace_id=$1`, v.WorkspaceID, n)
	if e != nil {
		return "", e
	}
	follow := ids
	if op == "create" {
		follow = append(append([]string{}, ids...), creator)
	}
	if _, assignmentsChanged := f["assignees"]; op == "create" || assignmentsChanged {
		for _, id := range follow {
			if e = t.autoFollow(ctx, v.ID, id); e != nil {
				return "", e
			}
		}
	}
	change := activity.Change{Kind: "task.changed", Field: "migration", After: activity.Value{Text: v.Title}}
	at := updated
	if op == "create" {
		change.Kind = "task.created"
		change.Field = ""
		at = created
	}
	_, e = t.migrationEvent(ctx, "", v.WorkspaceID, v.ID, "", t.actor.ID, change, at, at)
	return v.ID, e
}
