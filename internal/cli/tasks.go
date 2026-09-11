package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"acta/internal/activity"
	"acta/internal/client"
	"acta/internal/tasks"
	"github.com/spf13/cobra"
)

func runTask[T any](a *App, cmd *cobra.Command, run func(context.Context, *client.Client) (T, error), render func(T) string) error {
	c, e := a.authenticatedClient()
	if e != nil {
		return e
	}
	out, e := run(cmd.Context(), c)
	if e != nil {
		return e
	}
	return a.emit(out, render(out))
}
func (a *App) readTaskText(path string) (string, error) {
	var reader io.Reader = a.input
	if path != "-" {
		f, e := os.Open(path)
		if e != nil {
			return "", e
		}
		defer f.Close()
		reader = f
	}
	raw, e := io.ReadAll(io.LimitReader(reader, 100001))
	if e != nil {
		return "", e
	}
	if len(raw) > 100000 {
		return "", errors.New("Task text must not exceed 100,000 bytes")
	}
	return string(raw), nil
}
func (a *App) taskCommand() *cobra.Command {
	root := &cobra.Command{Use: "task", Short: "Find, inspect, create and edit workspace tasks"}
	root.AddCommand(a.commentCommand())
	var workspace string
	root.PersistentFlags().StringVarP(&workspace, "workspace", "w", "", "Workspace UUID or slug (see acta workspace list)")
	requireWorkspace := func() error {
		if strings.TrimSpace(workspace) == "" {
			return errors.New("Supply --workspace <UUID-or-slug>; use acta workspace list")
		}
		return nil
	}
	var searchArchived bool
	var searchCursor string
	search := &cobra.Command{Use: "search <query>", Short: "Find tasks across workspaces, including nested tasks and comments", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		q, _, e := tasks.NormalizeSearch(tasks.SearchQuery{IncludeArchived: searchArchived, Query: args[0], Workspace: workspace, Cursor: searchCursor})
		if e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.SearchPage, error) { return c.SearchTasks(ctx, q) }, renderTaskSearch)
	}}
	search.Flags().StringVar(&searchCursor, "cursor", "", "Returned search cursor; keep query and workspace unchanged")
	search.Flags().BoolVar(&searchArchived, "include-archived", false, "Include archived tasks in search")
	root.AddCommand(search)
	for _, archived := range []bool{true, false} {
		name := "archive"
		if !archived {
			name = "restore"
		}
		var version int64
		command := &cobra.Command{Use: name + " <reference-or-uuid>", Short: name + " a task subtree", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if version < 1 {
				return errors.New("Supply --version from versions.archived in task get")
			}
			return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Task, error) {
				return c.ArchiveTask(ctx, args[0], tasks.Archive{Archived: archived, Version: version})
			}, renderTask)
		}}
		command.Flags().Int64Var(&version, "version", 0, "Expected archived field version from task get")
		root.AddCommand(command)
	}
	f := tasks.Filter{}
	list := &cobra.Command{Use: "list", Short: "Search roots or direct children with filters and sorting", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		normalized, e := tasks.NormalizeFilter(f)
		if e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.SummaryPage, error) {
			return c.Tasks(ctx, workspace, normalized)
		}, renderTaskPage)
	}}
	list.Flags().StringVar(&f.Board, "board", "tasks", "tasks, backlog, or * for both; ignored for direct children")
	list.Flags().StringSliceVar(&f.Priorities, "priority", nil, "none, low, medium, high, urgent; match any")
	list.Flags().StringSliceVar(&f.Types, "type", nil, "none, bug, chore, feature; match any")
	list.Flags().StringSliceVar(&f.Sizes, "size", nil, "none, xs, s, m, l, xl; relative effort")
	list.Flags().BoolVar(&f.Archived, "archived", false, "List only archived tasks")
	list.Flags().StringVar(&f.State, "state", "all", "all, unfinished or completed")
	list.Flags().StringVar(&f.Parent, "parent", "", "Parent UUID or reference; omitted lists roots only")
	list.Flags().StringVar(&f.Query, "query", "", "Title or reference search")
	list.Flags().StringSliceVar(&f.Statuses, "status", nil, "Status UUIDs; match any selected status")
	list.Flags().StringSliceVar(&f.Assignees, "assignee", nil, "Direct account UUIDs; match any selected assignee")
	list.Flags().BoolVar(&f.Unassigned, "unassigned", false, "Include unassigned tasks (OR with selected assignees)")
	list.Flags().StringVar(&f.Sort, "sort", "number", "number, title, status, priority, type, size, created or updated")
	list.Flags().StringVar(&f.Direction, "direction", "desc", "asc or desc")
	list.Flags().StringVar(&f.Cursor, "cursor", "", "Returned cursor; keep all other query options unchanged")
	list.Flags().StringVar(&f.Group, "group", "", "Group: assignee, agents, priority, type or size; requires --group-id")
	list.Flags().StringVar(&f.GroupID, "group-id", "", "Group ID from task groups, including unassigned")
	var childCursor string
	get := &cobra.Command{Use: "get <reference-or-uuid>", Short: "Inspect Markdown, assignments, field versions and direct subtasks", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Detail, error) {
			return c.Task(ctx, args[0], childCursor)
		}, renderTaskDetail)
	}}
	get.Flags().StringVar(&childCursor, "subtask-cursor", "", "Continue the direct-subtask summary page")
	var createIn tasks.Create
	var descriptionFile string
	create := &cobra.Command{Use: "create", Short: "Create a task or subtask", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		if strings.TrimSpace(createIn.Title) == "" {
			return errors.New("Supply --title")
		}
		in := createIn
		if cmd.Flags().Changed("description-file") {
			v, e := a.readTaskText(descriptionFile)
			if e != nil {
				return e
			}
			in.Description = v
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Task, error) {
			return c.CreateTask(ctx, workspace, in)
		}, renderTask)
	}}
	create.Flags().StringVar(&createIn.Priority, "priority", "none", "none, low, medium, high or urgent")
	create.Flags().StringVar(&createIn.Type, "type", "none", "none, bug, chore or feature")
	create.Flags().StringVar(&createIn.Size, "size", "none", "none, xs, s, m, l or xl; relative effort")
	create.Flags().StringVar(&createIn.Title, "title", "", "Task title")
	create.Flags().StringVar(&createIn.Description, "description", "", "Markdown description")
	create.Flags().StringVar(&descriptionFile, "description-file", "", "Read Markdown from a file, or - for stdin")
	create.MarkFlagsMutuallyExclusive("description", "description-file")
	create.Flags().StringVar(&createIn.Board, "board", "", "tasks or backlog; defaults to Tasks or the parent board")
	create.Flags().StringVar(&createIn.StatusID, "status", "", "Status UUID; defaults to the board entry status")
	create.Flags().StringVar(&createIn.ParentID, "parent", "", "Parent UUID or reference")
	create.Flags().StringSliceVar(&createIn.Assignees, "assignee", nil, "Account UUIDs; repeated or comma separated")
	var field, value, valueFile string
	var version int64
	var ids []string
	var clear bool
	edit := &cobra.Command{Use: "edit <reference-or-uuid>", Short: "Update one explicitly supplied field using its expected version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if version < 1 {
			return errors.New("Supply --version from task get")
		}
		switch field {
		case "title", "description", "status_id", "board", "parent_id", "assignees", "priority", "type", "size":
		default:
			return errors.New("Choose --field title, description, status_id, board, parent_id, assignees, priority, type or size")
		}
		var v any
		if field == "assignees" {
			if cmd.Flags().Changed("value") || cmd.Flags().Changed("value-file") || (!clear && !cmd.Flags().Changed("assignee")) {
				return errors.New("Supply --assignee UUIDs or --clear for assignments; do not supply text")
			}
			v = ids
			if clear || ids == nil {
				v = []string{}
			}
		} else {
			if cmd.Flags().Changed("assignee") {
				return errors.New("--assignee requires --field assignees")
			}
			if clear && field != "description" && field != "parent_id" && !tasks.IsProperty(field) {
				return errors.New("Only description, parent_id, assignees and metadata can be cleared")
			}
			if !clear && !cmd.Flags().Changed("value") && !cmd.Flags().Changed("value-file") {
				return errors.New("Supply --value, --value-file or --clear; omitting a value never clears a field")
			}
			v = value
			if clear {
				v = ""
			} else if cmd.Flags().Changed("value-file") {
				text, e := a.readTaskText(valueFile)
				if e != nil {
					return e
				}
				v = text
			}
		}
		raw, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Task, error) {
			return c.UpdateTask(ctx, args[0], tasks.Patch{Field: field, Version: version, Value: raw})
		}, renderTask)
	}}
	edit.Flags().StringVar(&field, "field", "", "title, description, status_id, board, parent_id, assignees, priority, type or size")
	edit.Flags().StringVar(&value, "value", "", "Explicit new text; empty clears optional fields")
	edit.Flags().StringVar(&valueFile, "value-file", "", "Read new text from a file, or - for stdin")
	edit.Flags().BoolVar(&clear, "clear", false, "Explicitly clear description, parent_id, assignees, priority, type or size")
	edit.MarkFlagsMutuallyExclusive("value", "value-file", "clear")
	edit.Flags().Int64Var(&version, "version", 0, "Expected field version")
	edit.Flags().StringSliceVar(&ids, "assignee", nil, "Complete replacement set of account UUIDs")
	edit.MarkFlagsMutuallyExclusive("assignee", "clear")
	statuses := &cobra.Command{Use: "statuses", Short: "Find status UUIDs and workflow defaults", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Config, error) { return c.TaskConfig(ctx, workspace) }, renderTaskConfig)
	}}
	var peopleQuery string
	people := &cobra.Command{Use: "people", Short: "Find assignable people and agents; narrow the query to resolve similar names", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) ([]tasks.Person, error) {
			return c.TaskPeople(ctx, workspace, peopleQuery)
		}, renderTaskPeople)
	}}
	people.Flags().StringVar(&peopleQuery, "query", "", "Username or display name search; returns at most 50 candidates")
	var group string
	groups := &cobra.Command{Use: "groups", Short: "Find assignment groups for task list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		if group != "assignee" && group != "agents" && !tasks.IsProperty(group) {
			return errors.New("Choose --group assignee, agents, priority, type or size")
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) ([]tasks.Group, error) {
			return c.TaskGroups(ctx, workspace, group)
		}, renderTaskGroups)
	}}
	groups.Flags().StringVar(&group, "group", "assignee", "assignee (humans), agents (your assignments), priority, type or size")
	var activityCursor string
	history := &cobra.Command{Use: "activity <reference-or-uuid>", Short: "Read grouped activity, newest first; does not mark it read", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (activity.Page, error) {
			return c.TaskActivity(ctx, args[0], activityCursor)
		}, renderActivity)
	}}
	history.Flags().StringVar(&activityCursor, "cursor", "", "Load older activity using the returned cursor")
	root.AddCommand(list, get, create, edit, statuses, people, groups, history)
	return root
}
func (a *App) workspaceCommand() *cobra.Command {
	root := &cobra.Command{Use: "workspace", Short: "Find accessible workspaces"}
	var offset int
	var query string
	list := &cobra.Command{Use: "list", Short: "List workspace names, slugs and UUIDs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if offset < 0 {
			return fmt.Errorf("--offset must be non-negative")
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (client.WorkspacePage, error) {
			return c.Workspaces(ctx, query, offset)
		}, renderWorkspaces)
	}}
	list.Flags().IntVar(&offset, "offset", 0, "Next offset from the previous page")
	list.Flags().StringVar(&query, "query", "", "Workspace name or slug search")
	root.AddCommand(list)
	return root
}
