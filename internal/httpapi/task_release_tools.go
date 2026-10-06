package httpapi

import (
	"context"
	"slices"
	"time"

	"acta/internal/auth"
	"acta/internal/tasks"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// releaseSummary omits the notes so listing many releases stays compact.
type releaseSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Codename  string    `json:"codename"`
	State     string    `json:"state"`
	Total     int64     `json:"total"`
	Finished  int64     `json:"finished"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}
type releaseInput struct {
	Workspace string `json:"workspace" jsonschema:"Workspace UUID or slug."`
	Release   string `json:"release" jsonschema:"Release UUID from releases_list."`
}

func (h *Handler) releaseTools(s *mcp.Server, grants []string) {
	no := false
	if slices.Contains(grants, auth.MCPTasksRead) {
		read := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}
		addTaskTool(s, &mcp.Tool{Name: "releases_list", Description: "List a workspace's releases in natural name order with state and progress, without notes. States: planned (exists, not yet worked toward), open (taking work), frozen (no new work starts; in-flight work finishes), released (shipped). total/finished count active tasks targeted directly at the release; finished means the task holds its board's completed status. Use release_get for notes.", Annotations: read}, func(ctx context.Context, in workspaceInput) (struct {
			Releases []releaseSummary `json:"releases"`
		}, error) {
			rs, e := h.management.TaskReleases(ctx, "", in.Workspace)
			out := []releaseSummary{}
			for _, r := range rs {
				out = append(out, releaseSummary{r.ID, r.Name, r.Codename, r.State, r.Total, r.Finished, r.Version, r.UpdatedAt})
			}
			return struct {
				Releases []releaseSummary `json:"releases"`
			}{out}, e
		})
		addTaskTool(s, &mcp.Tool{Name: "release_get", Description: "Read one release, including its Markdown notes, state, progress and version. List its tasks with tasks_list using releases=[id] and all_depths=true.", Annotations: read}, func(ctx context.Context, in releaseInput) (tasks.Release, error) {
			return h.management.TaskRelease(ctx, "", in.Workspace, in.Release)
		})
	}
	if slices.Contains(grants, auth.MCPTasksWrite) {
		addTaskTool(s, &mcp.Tool{Name: "release_create", Description: "Create a release. Requires Manage releases. Check releases_list first: names are unique per workspace, case-insensitively. Releases cannot be deleted. Creation is not idempotent: check before retrying an uncertain result.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Workspace   string `json:"workspace" jsonschema:"Workspace UUID or slug."`
			Name        string `json:"name" jsonschema:"Release name or version, e.g. 2026.9; 1–60 characters."`
			Codename    string `json:"codename,omitempty" jsonschema:"Optional codename, at most 60 characters."`
			State       string `json:"state,omitempty" jsonschema:"planned (default), open, frozen or released."`
			Description string `json:"description,omitempty" jsonschema:"Markdown notes, which also serve as the release notes."`
		}) (tasks.Release, error) {
			return h.management.CreateTaskRelease(ctx, "", in.Workspace, tasks.ReleaseCreate{Name: in.Name, Codename: in.Codename, State: in.State, Description: in.Description})
		})
		addTaskTool(s, &mcp.Tool{Name: "release_update", Description: "Change a release's name, codename, state or notes. Requires Manage releases. Supply only the fields to change, with the release's current version from releases_list or release_get. Any state transition is allowed, including backwards to undo a mistake. On release_changed, read the release again and reconcile before retrying.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Workspace   string  `json:"workspace" jsonschema:"Workspace UUID or slug."`
			Release     string  `json:"release" jsonschema:"Release UUID."`
			Version     int64   `json:"version" jsonschema:"The release's current version."`
			Name        *string `json:"name,omitempty"`
			Codename    *string `json:"codename,omitempty" jsonschema:"Empty string clears the codename."`
			State       *string `json:"state,omitempty" jsonschema:"planned, open, frozen or released."`
			Description *string `json:"description,omitempty" jsonschema:"Replacement Markdown notes."`
		}) (tasks.Release, error) {
			return h.management.UpdateTaskRelease(ctx, "", in.Workspace, in.Release, tasks.ReleaseUpdate{Name: in.Name, Codename: in.Codename, State: in.State, Description: in.Description, Version: in.Version})
		})
	}
}
