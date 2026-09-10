package client

import (
	"acta2/internal/activity"
	"acta2/internal/tasks"
	"acta2/internal/workspaces"
	"context"
	"net/url"
	"strconv"
)

func (c *Client) Tasks(ctx context.Context, workspace string, f tasks.Filter) (tasks.SummaryPage, error) {
	q := url.Values{"board": {f.Board}, "archived": {strconv.FormatBool(f.Archived)}, "summary": {"true"}, "parent": {f.Parent}, "state": {f.State}, "q": {f.Query}, "sort": {f.Sort}, "direction": {f.Direction}, "cursor": {f.Cursor}, "group": {f.Group}, "group_id": {f.GroupID}}
	for _, id := range f.Statuses {
		q.Add("status", id)
	}
	for _, id := range f.Assignees {
		q.Add("assignee", id)
	}
	for key, values := range map[string][]string{"priority": f.Priorities, "type": f.Types, "size": f.Sizes} {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	if f.Unassigned {
		q.Set("unassigned", "true")
	}
	var out tasks.SummaryPage
	e := c.Call(ctx, "GET", "workspaces/"+url.PathEscape(workspace)+"/tasks?"+q.Encode(), nil, &out)
	return out, e
}
func (c *Client) Task(ctx context.Context, ref, cursor string) (tasks.Detail, error) {
	var out tasks.Detail
	e := c.Call(ctx, "GET", "tasks/"+url.PathEscape(ref)+"?include=subtasks&subtask_cursor="+url.QueryEscape(cursor), nil, &out)
	return out, e
}
func (c *Client) CreateTask(ctx context.Context, workspace string, in tasks.Create) (tasks.Task, error) {
	var out tasks.Task
	e := c.Call(ctx, "POST", "workspaces/"+url.PathEscape(workspace)+"/tasks", in, &out)
	return out, e
}
func (c *Client) UpdateTask(ctx context.Context, ref string, in tasks.Patch) (tasks.Task, error) {
	var out tasks.Task
	e := c.Call(ctx, "POST", "tasks/"+url.PathEscape(ref), in, &out)
	return out, e
}
func (c *Client) TaskConfig(ctx context.Context, workspace string) (tasks.Config, error) {
	var out tasks.Config
	e := c.Call(ctx, "GET", "workspaces/"+url.PathEscape(workspace)+"/task-config", nil, &out)
	return out, e
}
func (c *Client) TaskPeople(ctx context.Context, workspace, query string) ([]tasks.Person, error) {
	var out struct {
		People []tasks.Person `json:"people"`
	}
	e := c.Call(ctx, "GET", "workspaces/"+url.PathEscape(workspace)+"/task-people?q="+url.QueryEscape(query), nil, &out)
	return out.People, e
}
func (c *Client) TaskGroups(ctx context.Context, workspace, group string) ([]tasks.Group, error) {
	var out struct {
		Groups []tasks.Group `json:"groups"`
	}
	e := c.Call(ctx, "GET", "workspaces/"+url.PathEscape(workspace)+"/task-groups?group="+url.QueryEscape(group), nil, &out)
	return out.Groups, e
}

type WorkspacePage struct {
	Workspaces []workspaces.Workspace `json:"workspaces"`
	More       bool                   `json:"more"`
	NextOffset int                    `json:"next_offset"`
}

func (c *Client) Workspaces(ctx context.Context, query string, offset int) (WorkspacePage, error) {
	var out WorkspacePage
	e := c.Call(ctx, "GET", "workspaces?q="+url.QueryEscape(query)+"&offset="+strconv.Itoa(offset), nil, &out)
	out.NextOffset = offset + len(out.Workspaces)
	return out, e
}

func (c *Client) TaskActivity(ctx context.Context, ref, cursor string) (activity.Page, error) {
	var out activity.Page
	e := c.Call(ctx, "GET", "tasks/"+url.PathEscape(ref)+"/activity?cursor="+url.QueryEscape(cursor), nil, &out)
	return out, e
}

func (c *Client) SearchTasks(ctx context.Context, q tasks.SearchQuery) (tasks.SearchPage, error) {
	var out tasks.SearchPage
	v := url.Values{"include_archived": {strconv.FormatBool(q.IncludeArchived)}, "q": {q.Query}, "workspace": {q.Workspace}, "cursor": {q.Cursor}}
	e := c.Call(ctx, "GET", "tasks/search?"+v.Encode(), nil, &out)
	return out, e
}

func (c *Client) ArchiveTask(ctx context.Context, ref string, in tasks.Archive) (tasks.Task, error) {
	var out tasks.Task
	e := c.Call(ctx, "POST", "tasks/"+url.PathEscape(ref)+"/archive", in, &out)
	return out, e
}
