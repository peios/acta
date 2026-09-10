package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"unicode"

	"acta2/internal/client"
	"acta2/internal/tasks"
)

// Untrusted task text must not execute terminal control sequences or break rows.
func terminalText(v string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			if multiline && (r == '\n' || r == '\t') {
				return r
			}
			return ' '
		}
		return r
	}, v)
}
func table(headers []string, rows [][]string) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		clean := make([]string, len(row))
		for i, v := range row {
			clean[i] = terminalText(v, false)
		}
		fmt.Fprintln(w, strings.Join(clean, "\t"))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}
func assignmentNames(people []tasks.Person) string {
	names := []string{}
	for _, p := range people {
		names = append(names, "@"+p.Username)
	}
	if len(names) == 0 {
		return "Unassigned"
	}
	return strings.Join(names, ", ")
}
func renderTaskPage(p tasks.SummaryPage) string {
	rows := [][]string{}
	for _, t := range p.Tasks {
		rows = append(rows, []string{t.Reference, t.Status.Name, t.Title, tasks.PropertyLabel("priority", t.Priority), tasks.PropertyLabel("type", t.Type), tasks.PropertyLabel("size", t.Size), assignmentNames(t.Assignees)})
	}
	out := table([]string{"TASK", "STATUS", "TITLE", "PRIORITY", "TYPE", "SIZE", "ASSIGNEES"}, rows)
	if p.Total == 0 {
		out = "No matching tasks."
	} else {
		out += fmt.Sprintf("\n\n%d of %d tasks", len(p.Tasks), p.Total)
	}
	if p.More {
		out += "\nNext cursor: " + p.Cursor
	}
	return out
}
func renderTask(t tasks.Task) string {
	out := fmt.Sprintf("%s  %s\nID: %s\nWorkspace: %s\nStatus: %s\nParent: %s\nAssignees: %s", t.Reference, t.Title, t.ID, t.WorkspaceID, t.StatusID, t.ParentID, assignmentNames(t.Assignees))
	for _, p := range []struct{ name, value string }{{"priority", t.Priority}, {"type", t.Type}, {"size", t.Size}} {
		out += "\n" + p.name + ": " + tasks.PropertyLabel(p.name, p.value)
	}
	out += fmt.Sprintf("\nArchived: %t", t.Archived)
	out += "\nField versions:"
	for _, field := range []string{"title", "description", "status_id", "parent_id", "assignees", "priority", "type", "size", "archived"} {
		out += fmt.Sprintf("\n  %s: %d", field, t.Versions[field])
	}
	return terminalText(out, true)
}
func renderTaskDetail(d tasks.Detail) string {
	out := renderTask(d.Task) + "\nStatus name: " + terminalText(d.Status.Name, false)
	if len(d.Ancestors) > 0 {
		refs := []string{}
		for _, p := range d.Ancestors {
			refs = append(refs, p.Reference)
		}
		out += "\nAncestors: " + strings.Join(refs, " / ")
	}
	out += "\n\nDescription\n" + terminalText(d.Description, true)
	if d.Description == "" {
		out += "(empty)"
	}
	if len(d.DescendantAssignees) > 0 {
		out += "\n\nAssigned through subtasks: " + terminalText(assignmentNames(d.DescendantAssignees), false)
	}
	out += "\n\nSubtasks\n" + renderTaskPage(d.Subtasks)
	if d.Subtasks.More {
		out += "\nContinue with task get " + d.Reference + " --subtask-cursor <cursor>"
	}
	return out
}
func renderTaskConfig(c tasks.Config) string {
	rows := [][]string{}
	for _, s := range c.Statuses {
		kind := ""
		board := tasks.Board{Creation: c.Creation, Completed: c.Completed}
		for _, b := range c.Boards {
			if b.Slug == s.Board {
				board = b
			}
		}
		if s.ID == board.Creation {
			kind = "Creation"
		}
		if s.ID == board.Completed {
			kind = "Completed"
		}
		rows = append(rows, []string{board.Name, s.Name, s.ID, kind})
	}
	return "Prefix: " + c.Prefix + "\n\n" + table([]string{"BOARD", "STATUS", "ID", "DEFAULT"}, rows)
}
func renderTaskPeople(people []tasks.Person) string {
	rows := [][]string{}
	for _, p := range people {
		name := p.Username
		if p.DisplayName != nil {
			name = *p.DisplayName
		}
		kind := "Human"
		if p.Agent {
			kind = "Agent"
		}
		rows = append(rows, []string{name, "@" + p.Username, p.ID, kind})
	}
	if len(rows) == 0 {
		return "No matching people or agents."
	}
	return table([]string{"NAME", "USERNAME", "ID", "TYPE"}, rows) + "\n\nUp to 50 results; narrow --query if needed."
}
func renderTaskGroups(groups []tasks.Group) string {
	rows := [][]string{}
	for _, g := range groups {
		available := "Yes"
		if !g.Available {
			available = "No"
		}
		rows = append(rows, []string{g.Name, g.ID, available})
	}
	return table([]string{"GROUP", "ID", "CAN ASSIGN"}, rows)
}
func renderWorkspaces(p client.WorkspacePage) string {
	rows := [][]string{}
	for _, w := range p.Workspaces {
		rows = append(rows, []string{w.Name, w.Slug, w.ID})
	}
	out := table([]string{"WORKSPACE", "SLUG", "ID"}, rows)
	if len(rows) == 0 {
		out = "No matching workspaces."
	}
	if p.More {
		out += fmt.Sprintf("\n\nContinue with --offset %d", p.NextOffset)
	}
	return out
}

func renderTaskSearch(p tasks.SearchPage) string {
	var b strings.Builder
	for _, r := range p.Tasks {
		fmt.Fprintf(&b, "%s  %s  [%s] · %s\n", r.Reference, r.Title, r.Status.Name, r.WorkspaceName)
		if r.Archived {
			b.WriteString("  Archived\n")
		}
		if r.Source == "comment" || r.Source == "description" {
			b.WriteString("  " + r.Source + ": ")
			for _, part := range r.Excerpt {
				b.WriteString(part.Text)
			}
			b.WriteString("\n")
		}
	}
	if len(p.Tasks) == 0 {
		b.WriteString("No matching tasks.\n")
	}
	if p.More {
		fmt.Fprintf(&b, "\nMore results: --cursor %s\n", p.Cursor)
	}
	return b.String()
}
