package postgres

import (
	"acta/internal/accounts"
	"acta/internal/activity"
	"acta/internal/auth"
	"acta/internal/tasks"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const taskColumns = `t.id::text,t.workspace_id::text,t.number,c.prefix||'-'||t.number,t.title,t.description,t.status_id::text,COALESCE(t.parent_id::text,''),t.title_version,t.description_version,t.status_version,t.parent_version,t.assignees_version,t.priority,t.type,t.size,t.priority_version,t.type_version,t.size_version,t.created_at,t.updated_at,t.archived_at,t.archive_version,(SELECT count(*) FROM tasks child WHERE child.parent_id=t.id AND (t.archived_at IS NOT NULL OR child.archived_at IS NULL)),(SELECT count(*) FROM documents doc WHERE doc.task_id=t.id),(SELECT board FROM task_statuses WHERE id=t.status_id)`

func scanTask(row pgx.Row, extra ...any) (tasks.Task, error) {
	v := tasks.Task{Assignees: []tasks.Person{}, DescendantAssignees: []tasks.Person{}, Ancestors: []tasks.Source{}, Versions: map[string]int64{}}
	var a, b, c, d, e, pv, tv, sv, av int64
	args := []any{&v.ID, &v.WorkspaceID, &v.Number, &v.Reference, &v.Title, &v.Description, &v.StatusID, &v.ParentID, &a, &b, &c, &d, &e, &v.Priority, &v.Type, &v.Size, &pv, &tv, &sv, &v.CreatedAt, &v.UpdatedAt, &v.ArchivedAt, &av, &v.Children, &v.DocumentCount, &v.Board}
	err := row.Scan(append(args, extra...)...)
	v.Archived = v.ArchivedAt != nil
	v.Versions = map[string]int64{"archived": av, "title": a, "description": b, "status_id": c, "parent_id": d, "assignees": e, "priority": pv, "type": tv, "size": sv}
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrNotFound
	}
	return v, err
}

var referencePattern = regexp.MustCompile(`^([A-Za-z]{2,10})-([1-9][0-9]*)$`)

func (t workspaceTx) TaskGet(ctx context.Context, ref string) (tasks.Task, error) {
	var v tasks.Task
	var e error
	if match := referencePattern.FindStringSubmatch(ref); match != nil {
		n, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			return v, auth.ErrNotFound
		}
		v, e = scanTask(t.tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks t JOIN task_settings c ON c.workspace_id=t.workspace_id JOIN task_prefixes p ON p.workspace_id=t.workspace_id WHERE p.prefix=$1 AND t.number=$2`, strings.ToUpper(match[1]), n))
	} else {
		v, e = scanTask(t.tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks t JOIN task_settings c ON c.workspace_id=t.workspace_id WHERE t.id::text=$1`, ref))
	}
	if e != nil {
		return v, e
	}
	e = t.decorateTask(ctx, &v, true)
	return v, e
}
func (t workspaceTx) TaskList(ctx context.Context, w string, f tasks.Filter) (tasks.Page, error) {
	out := tasks.Page{Tasks: []tasks.Task{}}
	parent := f.Parent
	if parent != "" {
		p, e := t.TaskGet(ctx, parent)
		if e != nil || p.WorkspaceID != w {
			return out, auth.ErrNotFound
		}
		parent = p.ID
	}
	cursor, e := tasks.DecodeCursor(f)
	if e != nil {
		return out, e
	}
	expression, cast := "t.number", "bigint"
	switch f.Sort {
	case "priority", "type", "size":
		expression = "CASE t." + f.Sort
		for i, o := range tasks.PropertyOptions(f.Sort) {
			expression += " WHEN '" + o.Value + "' THEN " + strconv.Itoa(i)
		}
		expression += " ELSE 0 END"
		cast = "integer"
	case "title":
		expression, cast = `lower(t.title) COLLATE "C"`, `text COLLATE "C"`
	case "status":
		expression, cast = "s.position", "integer"
	case "created":
		expression, cast = "t.created_at", "timestamptz"
	case "updated":
		expression, cast = "t.updated_at", "timestamptz"
	}
	direction, comparison := "DESC", "<"
	if f.Direction == "asc" {
		direction, comparison = "ASC", ">"
	}
	valueExpression := "(" + expression + ")::text"
	if f.Sort == "created" || f.Sort == "updated" {
		valueExpression = `to_char(` + expression + ` AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`
	}
	keys := []string{}
	matching := ` FROM tasks t JOIN task_settings c ON c.workspace_id=t.workspace_id JOIN task_statuses s ON s.id=t.status_id JOIN task_boards b ON b.workspace_id=t.workspace_id AND b.slug=s.board WHERE t.workspace_id=$1 AND ($2<>'' OR t.parent_id IS NULL OR ($15<>'*' AND EXISTS(SELECT 1 FROM tasks parent JOIN task_statuses ps ON ps.id=parent.status_id WHERE parent.id=t.parent_id AND ps.board<>s.board)) ARCHIVE_ROOT) AND ($2='' OR t.parent_id::text=$2) AND ($3='all' OR ($3='completed' AND t.status_id=b.completed_status) OR (($3='' OR $3='unfinished') AND t.status_id IS DISTINCT FROM b.completed_status)) AND ($4='' OR position(lower($4) in lower(t.title))>0 OR position(upper($4) in c.prefix||'-'||t.number)>0) AND ($5=0 OR t.number<$5)
AND (COALESCE(cardinality($6::uuid[]),0)=0 OR t.status_id=ANY($6::uuid[]))
AND ((COALESCE(cardinality($7::uuid[]),0)=0 AND NOT $8)
  OR EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id=t.id AND a.account_id=ANY($7::uuid[]))
  OR ($8 AND NOT EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id=t.id)))`
	matching += `
AND ($9='' OR ($9='priority' AND t.priority=$10) OR ($9='type' AND t.type=$10) OR ($9='size' AND t.size=$10) OR ($10='unassigned' AND NOT EXISTS(SELECT 1 FROM task_assignees ga WHERE ga.task_id=t.id))
 OR ($10<>'unassigned' AND EXISTS(SELECT 1 FROM task_assignees ga JOIN accounts gp ON gp.id=ga.account_id
 WHERE ga.task_id=t.id AND (($9='assignee' AND COALESCE(gp.parent_id,gp.id)::text=$10)
 OR ($9='agents' AND gp.id::text=$10 AND (gp.id::text=$11 OR gp.parent_id::text=$11))))))`
	matching += ` AND ($2<>'' OR $15='*' OR s.board=$15) AND (COALESCE(cardinality($12::text[]),0)=0 OR t.priority=ANY($12::text[])) AND (COALESCE(cardinality($13::text[]),0)=0 OR t.type=ANY($13::text[])) AND (COALESCE(cardinality($14::text[]),0)=0 OR t.size=ANY($14::text[]))`
	root := ""
	if f.Archived {
		root = " OR NOT EXISTS(SELECT 1 FROM tasks parent WHERE parent.id=t.parent_id AND parent.archived_at IS NOT NULL)"
		matching += " AND t.archived_at IS NOT NULL"
	} else {
		matching += " AND t.archived_at IS NULL"
	}
	matching = strings.ReplaceAll(matching, "ARCHIVE_ROOT", root)
	owner := t.actor.ID
	if t.actor.ParentID != nil {
		owner = *t.actor.ParentID
	}
	if e = t.tx.QueryRow(ctx, `SELECT count(*)`+matching, w, parent, f.State, f.Query, int64(0), f.Statuses, f.Assignees, f.Unassigned, f.Group, f.GroupID, owner, f.Priorities, f.Types, f.Sizes, f.Board).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := t.tx.Query(ctx, `SELECT `+strings.Replace(taskColumns, "t.description", "''", 1)+`,`+valueExpression+matching+`
AND ($17::bigint=0 OR (`+expression+`,t.number) `+comparison+` ( $16::`+cast+`,$17::bigint))
ORDER BY `+expression+` `+direction+`,t.number `+direction+` LIMIT 51`, w, parent, f.State, f.Query, f.Before, f.Statuses, f.Assignees, f.Unassigned, f.Group, f.GroupID, owner, f.Priorities, f.Types, f.Sizes, f.Board, cursor.Value, cursor.Number)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var key string
		v, e := scanTask(rows, &key)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Tasks = append(out.Tasks, v)
		keys = append(keys, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Tasks) > 50 {
		out.More = true
		out.Tasks = out.Tasks[:50]
	}
	if e = t.taskAssignments(ctx, w, out.Tasks); e != nil {
		return out, e
	}
	if len(out.Tasks) > 0 {
		out.Next = out.Tasks[len(out.Tasks)-1].Number
		out.Cursor = tasks.Cursor{Sort: f.Sort, Direction: f.Direction, Value: keys[len(out.Tasks)-1], Number: out.Next}.Encode()
	}
	return out, nil
}
func (t workspaceTx) TaskCreate(ctx context.Context, w string, in tasks.Create) (tasks.Task, error) {
	c, e := t.TaskConfig(ctx, w)
	if e != nil {
		return tasks.Task{}, e
	}
	if in.Board == "" && in.ParentID != "" && in.StatusID == "" {
		p, err := t.TaskGet(ctx, in.ParentID)
		if err != nil {
			return tasks.Task{}, err
		}
		in.Board = p.Board
	}
	if in.Board != "" || in.StatusID == "" {
		board, err := tasks.BoardSlug(in.Board)
		if err != nil {
			return tasks.Task{}, err
		}
		if in.StatusID == "" {
			for _, b := range c.Boards {
				if b.Slug == board {
					in.StatusID = b.Creation
				}
			}
		}
		for _, s := range c.Statuses {
			if s.ID == in.StatusID && s.Board != board {
				return tasks.Task{}, &accounts.FieldError{Field: "status_id", Message: "Choose a status on the selected board."}
			}
		}
	}
	var n int64
	e = t.tx.QueryRow(ctx, `UPDATE task_settings SET next_number=next_number+1,revision=revision+1 WHERE workspace_id=$1 RETURNING next_number-1`, w).Scan(&n)
	if e != nil {
		return tasks.Task{}, e
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	_, e = t.tx.Exec(ctx, `INSERT INTO tasks(id,workspace_id,number,title,description,status_id,parent_id,created_by,created_at,updated_at,priority,type,size) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$9,$10,$11,$12)`, id, w, n, in.Title, in.Description, in.StatusID, in.ParentID, t.actor.ID, now, in.Priority, in.Type, in.Size)
	if e != nil {
		return tasks.Task{}, taskDBError(e)
	}
	if e = t.setTaskAssignees(ctx, id, in.Assignees); e != nil {
		return tasks.Task{}, e
	}
	out, e := t.TaskGet(ctx, id)
	if e != nil {
		return out, e
	}
	e = t.recordActivity(ctx, w, "task", id, activity.Change{Kind: "task.created", After: activity.Value{Text: out.Title}})
	return out, e
}
func (t workspaceTx) setTaskAssignees(ctx context.Context, id string, people []string) error {
	if _, e := t.tx.Exec(ctx, `DELETE FROM task_assignees WHERE task_id=$1`, id); e != nil {
		return e
	}
	for _, p := range people {
		if _, e := t.tx.Exec(ctx, `INSERT INTO task_assignees VALUES($1,$2)`, id, p); e != nil {
			return e
		}
	}
	return nil
}
func (t workspaceTx) TaskPatch(ctx context.Context, v tasks.Task, in tasks.Patch) (tasks.Task, error) {
	columns := map[string]string{"title": "title", "description": "description", "status_id": "status", "parent_id": "parent", "assignees": "assignees", "priority": "priority", "type": "type", "size": "size"}
	col, ok := columns[in.Field]
	if !ok {
		return v, auth.ErrForbidden
	}
	var value string
	var ids []string
	equal := false
	if in.Field == "assignees" {
		_ = json.Unmarshal(in.Value, &ids)
		before := []string{}
		for _, p := range v.Assignees {
			before = append(before, p.ID)
		}
		slices.Sort(before)
		slices.Sort(ids)
		equal = slices.Equal(before, ids)
	} else {
		_ = json.Unmarshal(in.Value, &value)
		equal = value == map[string]string{"priority": v.Priority, "type": v.Type, "size": v.Size, "title": v.Title, "description": v.Description, "status_id": v.StatusID, "parent_id": v.ParentID}[in.Field]
	}
	if equal {
		return v, nil
	}
	if v.Versions[in.Field] != in.Version {
		return v, tasks.NewConflict(v, in.Field)
	}
	if in.Field == "parent_id" && value != "" {
		var cycle bool
		e := t.tx.QueryRow(ctx, `WITH RECURSIVE tree AS (SELECT id FROM tasks WHERE id=$1 UNION ALL SELECT c.id FROM tasks c JOIN tree ON c.parent_id=tree.id) SELECT EXISTS(SELECT 1 FROM tree WHERE id::text=$2)`, v.ID, value).Scan(&cycle)
		if e != nil {
			return v, e
		}
		if cycle {
			return v, &accounts.FieldError{Field: "parent_id", Message: "A task cannot be its own ancestor."}
		}
	}
	if in.Field == "assignees" {
		if e := t.setTaskAssignees(ctx, v.ID, ids); e != nil {
			return v, e
		}
	} else {
		expr := "$2"
		if in.Field == "parent_id" {
			expr = "NULLIF($2,'')::uuid"
		}
		if _, e := t.tx.Exec(ctx, `UPDATE tasks SET `+in.Field+`=`+expr+` WHERE id=$1`, v.ID, value); e != nil {
			return v, taskDBError(e)
		}
	}
	if _, e := t.tx.Exec(ctx, `UPDATE tasks SET `+col+`_version=`+col+`_version+1,updated_at=now() WHERE id=$1`, v.ID); e != nil {
		return v, e
	}
	if e := t.taskChanged(ctx, v.WorkspaceID); e != nil {
		return v, e
	}
	out, e := t.TaskGet(ctx, v.ID)
	if e != nil {
		return out, e
	}
	e = t.taskActivityChange(ctx, v, out, in.Field)
	return out, e
}
