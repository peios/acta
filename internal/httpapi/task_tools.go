package httpapi

import (
	"context"
	"encoding/json"
	"slices"

	"acta2/internal/accounts"
	"acta2/internal/activity"
	"acta2/internal/auth"
	"acta2/internal/tasks"
	"acta2/internal/workspaces"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every task tool has the same typed success/error envelope. The SDK also emits
// its JSON as text for clients that do not consume structuredContent.
type toolOutcome[T any] struct {
	Data  *T       `json:"data,omitempty"`
	Error *Problem `json:"error,omitempty"`
}

func addTaskTool[In, Out any](s *mcp.Server, tool *mcp.Tool, run func(context.Context, In) (Out, error)) {
	mcp.AddTool(s, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, toolOutcome[Out], error) {
		out, err := run(ctx, in)
		if err != nil {
			_, p := classifyError(err)
			return &mcp.CallToolResult{IsError: true}, toolOutcome[Out]{Error: &p}, nil
		}
		return nil, toolOutcome[Out]{Data: &out}, nil
	})
}

type workspaceInput struct {
	Workspace string `json:"workspace" jsonschema:"Workspace UUID or exact current/previous slug; discover with workspaces_list."`
}
type listTasksInput struct {
	Board      string   `json:"board,omitempty" jsonschema:"tasks (default), backlog, or * for both. Parent queries include all direct children regardless of board. Global task_search always searches both."`
	Archived   bool     `json:"archived,omitempty" jsonschema:"List only archived tasks instead of active tasks. Archive roots include archived children whose parent is active."`
	Workspace  string   `json:"workspace" jsonschema:"Workspace UUID or exact current/previous slug."`
	Parent     string   `json:"parent,omitempty" jsonschema:"Task UUID or reference. Omit to list roots; set to list only direct children."`
	State      string   `json:"state,omitempty" jsonschema:"all (default), unfinished or completed; applies to each listed task independently."`
	Query      string   `json:"query,omitempty" jsonschema:"Search titles or task references within this hierarchy level."`
	Priorities []string `json:"priorities,omitempty" jsonschema:"none, low, medium, high, urgent; match any selected priority."`
	Types      []string `json:"types,omitempty" jsonschema:"none, bug, chore, feature; match any selected type."`
	Sizes      []string `json:"sizes,omitempty" jsonschema:"none, xs, s, m, l, xl; relative effort, not hours."`
	Statuses   []string `json:"statuses,omitempty" jsonschema:"Status UUIDs from task_statuses; matches any selected status."`
	Assignees  []string `json:"assignees,omitempty" jsonschema:"Direct account UUID assignments; matches any selected account."`
	Unassigned bool     `json:"unassigned,omitempty" jsonschema:"Include tasks with no direct assignments; OR with the assignees selection."`
	Sort       string   `json:"sort,omitempty" jsonschema:"number (default), title, status, priority, type, size, created or updated."`
	Direction  string   `json:"direction,omitempty" jsonschema:"asc or desc (default). Keep unchanged while paging."`
	Cursor     string   `json:"cursor,omitempty" jsonschema:"Opaque cursor returned by this query. Keep all other query options unchanged."`
	Group      string   `json:"group,omitempty" jsonschema:"Optional assignee, agents, priority, type or size grouping; requires group_id from task_groups."`
	GroupID    string   `json:"group_id,omitempty" jsonschema:"Group ID from task_groups, including unassigned."`
}

func (in listTasksInput) filter() tasks.Filter {
	state := in.State
	if state == "" {
		state = "all"
	}
	return tasks.Filter{Board: in.Board, Archived: in.Archived, Parent: in.Parent, State: state, Query: in.Query, Priorities: in.Priorities, Types: in.Types, Sizes: in.Sizes, Statuses: in.Statuses, Assignees: in.Assignees, Unassigned: in.Unassigned, Sort: in.Sort, Direction: in.Direction, Cursor: in.Cursor, Group: in.Group, GroupID: in.GroupID}
}

type updateTaskInput struct {
	Task      string    `json:"task" jsonschema:"Task UUID or reference, including a previous workspace prefix."`
	Field     string    `json:"field" jsonschema:"Exactly one of title, description, status_id, parent_id, assignees, board, priority, type or size."`
	Version   int64     `json:"version" jsonschema:"For board use versions.status_id. Otherwise positive expected version for this field, from task_get or the previous mutation result."`
	Text      *string   `json:"text,omitempty" jsonschema:"Required for text fields. Empty string explicitly clears description, parent_id or metadata. Priority: none/low/medium/high/urgent. Type: none/bug/chore/feature. Size: none/xs/s/m/l/xl (relative effort). Omit for assignees."`
	Assignees *[]string `json:"assignees,omitempty" jsonschema:"Required for field=assignees: complete replacement UUID array. [] explicitly removes all assignments. Omit for text fields."`
}

func (in updateTaskInput) patch() (tasks.Patch, error) {
	var value any
	if in.Field == "assignees" {
		if in.Assignees == nil || in.Text != nil {
			return tasks.Patch{}, &accounts.FieldError{Field: "assignees", Message: "Supply an explicit assignees array, without text; [] clears assignments."}
		}
		value = *in.Assignees
	} else {
		if in.Text == nil || in.Assignees != nil {
			return tasks.Patch{}, &accounts.FieldError{Field: "text", Message: "Supply explicit text, without assignees; an empty string clears optional text fields."}
		}
		value = *in.Text
	}
	raw, err := json.Marshal(value)
	return tasks.Patch{Field: in.Field, Version: in.Version, Value: raw}, err
}
func (h *Handler) taskTools(s *mcp.Server, grants []string) {
	h.commentTools(s, grants)
	no := false
	read := func(name, description string) *mcp.Tool {
		return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}
	}
	if slices.Contains(grants, auth.MCPTasksRead) {
		addTaskTool(s, read("task_search", "Find tasks across every hierarchy depth in accessible workspaces. Searches titles, descriptions and comments; exact references rank first. Returns one compact result per task with the best excerpt. Use task_get to inspect a match. Try alternative wording before assuming work is untracked; only search exhaustively when there is concrete reason to believe a task exists. Follow cursor while more=true, keeping query and workspace unchanged."), func(ctx context.Context, in tasks.SearchQuery) (tasks.SearchPage, error) {
			return h.management.SearchTasks(ctx, "", in)
		})
		addTaskTool(s, read("task_activity", "Read grouped task activity, newest first, without marking it read. Same-actor consecutive edits use a 60-second sliding window. Follow cursor for older entries. Descriptions have edit notices only; full revisions are not retained."), func(ctx context.Context, in struct {
			Task   string `json:"task" jsonschema:"Task UUID or reference."`
			Cursor string `json:"cursor,omitempty" jsonschema:"Older-page cursor from the previous result."`
		}) (activity.Page, error) {
			return h.management.TaskActivity(ctx, "", in.Task, in.Cursor)
		})
		addTaskTool(s, read("workspaces_list", "Find accessible workspaces. Returns UUIDs and slugs; follow next_offset while more=true."), func(ctx context.Context, in struct {
			Query  string `json:"query,omitempty"`
			Offset int    `json:"offset,omitempty"`
		}) (workspaceToolPage, error) {
			rows, more, e := h.management.WorkspaceList(ctx, "", in.Query, in.Offset)
			return workspaceToolPage{rows, more, in.Offset + len(rows)}, e
		})
		addTaskTool(s, read("tasks_list", "List one hierarchy level. For finding work across nested tasks or workspaces use task_search. Results omit descriptions and recursive sources. Filters are ANDed across categories; status and assignee selections each match any selected value. Board lists also show children whose parent is on the other board, retaining the parent link. Across both boards, roots remain true hierarchy roots. Follow cursor while more=true with the same query."), func(ctx context.Context, in listTasksInput) (tasks.SummaryPage, error) {
			return h.management.TaskSummaries(ctx, "", in.Workspace, in.filter())
		})
		addTaskTool(s, read("task_get", "Inspect Markdown, direct/descendant assignments, ancestors, field versions and a direct-subtask summary page, including completed children. Subtasks sort by task number ascending; follow subtasks.cursor with subtask_cursor."), func(ctx context.Context, in struct {
			Task   string `json:"task" jsonschema:"Task UUID or reference."`
			Cursor string `json:"subtask_cursor,omitempty"`
		}) (tasks.Detail, error) {
			return h.management.InspectTask(ctx, "", in.Task, in.Cursor)
		})
		addTaskTool(s, read("task_statuses", "Read both boards and their status names/UUIDs and entry/completed selections. Each status belongs to a board. Changing status can move a task between boards."), func(ctx context.Context, in workspaceInput) (tasks.Config, error) {
			return h.management.TaskConfig(ctx, "", in.Workspace)
		})
		addTaskTool(s, read("task_people", "Find assignable humans and agents by username or display name. Returns at most 50 candidates; narrow query as needed. Use returned UUIDs, never guess among similar names."), func(ctx context.Context, in struct {
			Workspace string `json:"workspace"`
			Query     string `json:"query,omitempty"`
		}) (struct {
			People []tasks.Person `json:"people"`
		}, error) {
			v, e := h.management.TaskPeople(ctx, "", in.Workspace, in.Query)
			return struct {
				People []tasks.Person `json:"people"`
			}{v}, e
		})
		addTaskTool(s, read("task_groups", "Resolve assignment or fixed metadata group IDs. assignee combines humans with their agents; agents contains unassigned, the current human and their agents. A multiply assigned task may appear in several groups."), func(ctx context.Context, in struct {
			Workspace string `json:"workspace"`
			Group     string `json:"group" jsonschema:"assignee, agents, priority, type or size"`
		}) (struct {
			Groups []tasks.Group `json:"groups"`
		}, error) {
			v, e := h.management.TaskGroups(ctx, "", in.Workspace, in.Group)
			return struct {
				Groups []tasks.Group `json:"groups"`
			}{v}, e
		})
	}
	if slices.Contains(grants, auth.MCPTasksWrite) {
		for _, archived := range []bool{true, false} {
			name, description := "task_archive", "Archive a task and its active subtree. Keeps content, status and assignments; archived tasks are read-only. Do not archive merely because work is complete."
			if !archived {
				name = "task_restore"
				description = "Restore an archived subtree to its previous location. Restore archived ancestors first. Separately archived descendants remain archived."
			}
			addTaskTool(s, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
				Task    string `json:"task" jsonschema:"Task UUID or reference."`
				Version int64  `json:"version" jsonschema:"Current versions.archived value from task_get."`
			}) (tasks.Task, error) {
				return h.management.ArchiveTask(ctx, "", in.Task, tasks.Archive{Archived: archived, Version: in.Version})
			})
		}
		addTaskTool(s, &mcp.Tool{Name: "task_create", Description: "Create a task or subtask. First try task_search with relevant terms and alternative wording; assume untracked if no clear match unless you have concrete reason to believe an existing task needs finding. Requires Create tasks. Omitted status uses the selected board entry status; board defaults to Tasks, or the parent board for subtasks. Parent may be a UUID or reference; assignees are account UUIDs. Assignment does not start agents. Creation is not idempotent: check before retrying an uncertain result.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Workspace   string   `json:"workspace" jsonschema:"Workspace UUID or slug."`
			Board       string   `json:"board,omitempty" jsonschema:"tasks or backlog. Omit when selecting a status directly; an explicit board and status must agree."`
			Priority    string   `json:"priority,omitempty" jsonschema:"none (default), low, medium, high or urgent."`
			Type        string   `json:"type,omitempty" jsonschema:"none (default), bug, chore or feature."`
			Size        string   `json:"size,omitempty" jsonschema:"none (default), xs, s, m, l or xl. Relative effort, not hours; no subtask roll-up."`
			Title       string   `json:"title"`
			Description string   `json:"description,omitempty" jsonschema:"Markdown; omitted means empty."`
			Status      string   `json:"status_id,omitempty"`
			Parent      string   `json:"parent,omitempty"`
			Assignees   []string `json:"assignees,omitempty"`
		}) (tasks.Task, error) {
			return h.management.CreateTask(ctx, "", in.Workspace, tasks.Create{Board: in.Board, Priority: in.Priority, Type: in.Type, Size: in.Size, Title: in.Title, Description: in.Description, StatusID: in.Status, ParentID: in.Parent, Assignees: in.Assignees})
		})
		addTaskTool(s, &mcp.Tool{Name: "task_update", Description: "Change one explicitly supplied field using its expected version. Other fields stay unchanged. Returns the saved task. On conflict, error.current contains the current field value/version: reconcile before retrying. Set field=board with text=tasks/backlog and versions.status_id to move to its entry status; choose status_id for a specific destination status. Moves preserve identity, history and parent links; children keep their statuses. Reparenting preserves task identity; assignment replacement never starts agents.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in updateTaskInput) (tasks.Task, error) {
			p, e := in.patch()
			if e != nil {
				return tasks.Task{}, e
			}
			return h.management.PatchTask(ctx, "", in.Task, p)
		})
	}
}

type workspaceToolPage struct {
	Workspaces []workspaces.Workspace `json:"workspaces"`
	More       bool                   `json:"more"`
	NextOffset int                    `json:"next_offset"`
}
