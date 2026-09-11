package postgres

import (
	"acta/internal/tasks"
	"context"
	"slices"
	"strconv"
	"strings"
)

// Load the assignment trees for a whole page in one query. Account/access
// projections are reused only inside this transaction, never across revisions.
func (t workspaceTx) taskAssignments(ctx context.Context, workspace string, values []tasks.Task) error {
	if len(values) == 0 {
		return nil
	}
	ids := make([]string, len(values))
	indexes := make(map[string]int, len(values))
	descendants := make([]map[string]tasks.Person, len(values))
	for i := range values {
		ids[i] = values[i].ID
		indexes[ids[i]] = i
		values[i].Assignees = []tasks.Person{}
		values[i].DescendantAssignees = []tasks.Person{}
		descendants[i] = map[string]tasks.Person{}
	}
	rows, e := t.tx.Query(ctx, `WITH RECURSIVE tree AS (
 SELECT id AS root,id,number,title,archived_at IS NOT NULL AS archived,0 AS depth FROM tasks WHERE id=ANY($1::uuid[])
 UNION ALL SELECT tree.root,c.id,c.number,c.title,tree.archived,tree.depth+1 FROM tasks c JOIN tree ON c.parent_id=tree.id WHERE tree.archived OR c.archived_at IS NULL
 ) SELECT tree.root::text,a.account_id::text,tree.id::text,tree.number,tree.title,tree.depth
 FROM tree JOIN task_assignees a ON a.task_id=tree.id ORDER BY tree.root,tree.depth,tree.number,a.account_id`, ids)
	if e != nil {
		return e
	}
	type assignment struct {
		root, person, task, title string
		number                    int64
		depth                     int
	}
	all := []assignment{}
	for rows.Next() {
		var r assignment
		if e = rows.Scan(&r.root, &r.person, &r.task, &r.number, &r.title, &r.depth); e != nil {
			rows.Close()
			return e
		}
		all = append(all, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	people := map[string]tasks.Person{}
	for _, r := range all {
		p, ok := people[r.person]
		if !ok {
			p, e = t.person(ctx, r.person, workspace)
			if e != nil {
				return e
			}
			people[r.person] = p
		}
		i := indexes[r.root]
		if r.depth == 0 {
			values[i].Assignees = append(values[i].Assignees, p)
			continue
		}
		d, ok := descendants[i][r.person]
		if !ok {
			d = p
		}
		prefix := strings.Split(values[i].Reference, "-")[0]
		d.Sources = append(d.Sources, tasks.Source{ID: r.task, Title: r.title, Reference: prefix + "-" + strconv.FormatInt(r.number, 10)})
		descendants[i][r.person] = d
	}
	for i := range values {
		for _, p := range descendants[i] {
			values[i].DescendantAssignees = append(values[i].DescendantAssignees, p)
		}
		slices.SortFunc(values[i].DescendantAssignees, func(a, b tasks.Person) int { return strings.Compare(a.Username, b.Username) })
	}
	return nil
}

func (t workspaceTx) person(ctx context.Context, id, w string) (tasks.Person, error) {
	a, e := t.Account(ctx, id)
	if e != nil {
		return tasks.Person{}, e
	}
	access, e := t.Access(ctx, a, w)
	owner := ""
	if a.ParentID != nil {
		owner = *a.ParentID
	}
	return tasks.Person{ID: id, OwnerID: owner, Username: a.Handle(), DisplayName: a.DisplayName, Agent: a.IsAgent(), Available: a.Available() && access.Allowed, Sources: []tasks.Source{}}, e
}
func (t workspaceTx) decorateTask(ctx context.Context, v *tasks.Task, detail bool) error {
	values := []tasks.Task{*v}
	if e := t.taskAssignments(ctx, v.WorkspaceID, values); e != nil {
		return e
	}
	*v = values[0]
	if !detail {
		return nil
	}
	prefix := strings.Split(v.Reference, "-")[0]
	rows, e := t.tx.Query(ctx, `WITH RECURSIVE parents AS (SELECT id,parent_id,number,title,0 depth FROM tasks WHERE id::text=$1 UNION ALL SELECT t.id,t.parent_id,t.number,t.title,p.depth+1 FROM tasks t JOIN parents p ON p.parent_id=t.id) SELECT id::text,number,title FROM parents ORDER BY depth DESC`, v.ParentID)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var s tasks.Source
		var n int64
		if e = rows.Scan(&s.ID, &n, &s.Title); e != nil {
			return e
		}
		s.Reference = prefix + "-" + strconv.FormatInt(n, 10)
		v.Ancestors = append(v.Ancestors, s)
	}
	return rows.Err()
}
func (t workspaceTx) TaskPeople(ctx context.Context, w, q string) ([]tasks.Person, error) {
	return t.taskPeople(ctx, w, q, false)
}
func (t workspaceTx) TaskGroupPeople(ctx context.Context, w string) ([]tasks.Person, error) {
	return t.taskPeople(ctx, w, "", true)
}
func (t workspaceTx) taskPeople(ctx context.Context, w, q string, grouped bool) ([]tasks.Person, error) {
	rows, e := t.tx.Query(ctx, `SELECT a.id::text FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE ($1='' OR position(lower($1) in lower(COALESCE(p.username||'/','')||a.username||' '||COALESCE(a.display_name,'')))>0)
 AND ((a.disabled_at IS NULL AND NOT a.pending AND (a.parent_id IS NULL OR (p.disabled_at IS NULL AND NOT p.pending))
 AND (EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=$2 AND m.account_id=COALESCE(a.parent_id,a.id)) OR EXISTS(SELECT 1 FROM superuser_accounts s WHERE s.id=COALESCE(a.parent_id,a.id)))
 AND (a.parent_id IS NULL OR COALESCE((SELECT all_workspaces FROM agent_workspace_access WHERE account_id=a.id),true) OR EXISTS(SELECT 1 FROM agent_workspace_policies p WHERE p.account_id=a.id AND p.workspace_id=$2 AND p.selected))
 ) OR ($3 AND EXISTS(SELECT 1 FROM task_assignees ta JOIN tasks task ON task.id=ta.task_id JOIN accounts assigned ON assigned.id=ta.account_id WHERE task.workspace_id=$2 AND (assigned.id=a.id OR assigned.parent_id=a.id))))
 ORDER BY a.username,a.id LIMIT CASE WHEN $3 THEN NULL ELSE 50 END`, q, w, grouped)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []tasks.Person{}
	for _, id := range ids {
		p, e := t.person(ctx, id, w)
		if e != nil {
			return nil, e
		}
		if p.Available || grouped {
			out = append(out, p)
		}
		if !grouped && len(out) == 50 {
			break
		}
	}
	return out, nil
}
