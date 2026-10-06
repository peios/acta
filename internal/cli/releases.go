package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"acta/internal/client"
	"acta/internal/tasks"
	"github.com/spf13/cobra"
)

func (a *App) releaseCommand() *cobra.Command {
	root := &cobra.Command{Use: "release", Short: "List, inspect, create and edit workspace releases"}
	var workspace string
	root.PersistentFlags().StringVarP(&workspace, "workspace", "w", "", "Workspace UUID or slug (see acta workspace list)")
	requireWorkspace := func() error {
		if strings.TrimSpace(workspace) == "" {
			return errors.New("Supply --workspace <UUID-or-slug>; use acta workspace list")
		}
		return nil
	}
	list := &cobra.Command{Use: "list", Short: "List releases with state and progress, in name order", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) ([]tasks.Release, error) {
			return c.TaskReleases(ctx, workspace)
		}, renderReleases)
	}}
	get := &cobra.Command{Use: "get <uuid>", Short: "Inspect a release's notes, state, progress and version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Release, error) {
			return c.TaskRelease(ctx, workspace, args[0])
		}, renderRelease)
	}}
	var createIn tasks.ReleaseCreate
	var notesFile string
	create := &cobra.Command{Use: "create", Short: "Create a release; releases cannot be deleted", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		in := createIn
		if cmd.Flags().Changed("notes-file") {
			v, e := a.readTaskText(notesFile)
			if e != nil {
				return e
			}
			in.Description = v
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Release, error) {
			return c.CreateTaskRelease(ctx, workspace, in)
		}, renderRelease)
	}}
	create.Flags().StringVar(&createIn.Name, "name", "", "Release name or version, e.g. 2026.9")
	create.Flags().StringVar(&createIn.Codename, "codename", "", "Optional codename")
	create.Flags().StringVar(&createIn.State, "state", "planned", "planned, open, frozen or released")
	create.Flags().StringVar(&createIn.Description, "notes", "", "Markdown release notes")
	create.Flags().StringVar(&notesFile, "notes-file", "", "Read Markdown notes from a file, or - for stdin")
	create.MarkFlagsMutuallyExclusive("notes", "notes-file")
	var name, codename, state, notes string
	var version int64
	edit := &cobra.Command{Use: "edit <uuid>", Short: "Change the supplied fields using the release's current version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if e := requireWorkspace(); e != nil {
			return e
		}
		if version < 1 {
			return errors.New("Supply --version from release get")
		}
		in := tasks.ReleaseUpdate{Version: version}
		for flag, target := range map[string]**string{"name": &in.Name, "codename": &in.Codename, "state": &in.State, "notes": &in.Description} {
			if cmd.Flags().Changed(flag) {
				v, _ := cmd.Flags().GetString(flag)
				*target = &v
			}
		}
		if cmd.Flags().Changed("notes-file") {
			v, e := a.readTaskText(notesFile)
			if e != nil {
				return e
			}
			in.Description = &v
		}
		if in.Name == nil && in.Codename == nil && in.State == nil && in.Description == nil {
			return errors.New("Supply at least one of --name, --codename, --state, --notes or --notes-file")
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (tasks.Release, error) {
			return c.UpdateTaskRelease(ctx, workspace, args[0], in)
		}, renderRelease)
	}}
	edit.Flags().StringVar(&name, "name", "", "New name")
	edit.Flags().StringVar(&codename, "codename", "", "New codename; empty clears it")
	edit.Flags().StringVar(&state, "state", "", "planned, open, frozen or released; any transition is allowed")
	edit.Flags().StringVar(&notes, "notes", "", "Replacement Markdown notes")
	edit.Flags().StringVar(&notesFile, "notes-file", "", "Read replacement notes from a file, or - for stdin")
	edit.MarkFlagsMutuallyExclusive("notes", "notes-file")
	edit.Flags().Int64Var(&version, "version", 0, "Expected release version")
	root.AddCommand(list, get, create, edit)
	return root
}
func releaseLabel(r *tasks.ReleaseRef) string {
	if r == nil {
		return "None"
	}
	return r.Name
}
func releaseName(r tasks.Release) string {
	if r.Codename == "" {
		return r.Name
	}
	return r.Name + " “" + r.Codename + "”"
}
func renderReleases(rs []tasks.Release) string {
	if len(rs) == 0 {
		return "No releases."
	}
	rows := [][]string{}
	for _, r := range rs {
		rows = append(rows, []string{releaseName(r), r.State, fmt.Sprintf("%d/%d", r.Finished, r.Total), r.ID, fmt.Sprint(r.Version)})
	}
	return table([]string{"RELEASE", "STATE", "FINISHED", "ID", "VERSION"}, rows)
}
func renderRelease(r tasks.Release) string {
	out := fmt.Sprintf("%s\nID: %s\nState: %s\nFinished: %d of %d tasks\nVersion: %d\n\nNotes\n", releaseName(r), r.ID, r.State, r.Finished, r.Total, r.Version)
	if r.Description == "" {
		return terminalText(out+"(empty)", true)
	}
	return terminalText(out+r.Description, true)
}
