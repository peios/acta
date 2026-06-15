package mcpcfg

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/peios/acta/internal/store"
)

// guideTemplate is the conventions document served as acta://guide. It is
// hardcoded and ships with each release — Acta's equivalent of a system prompt,
// the same for every instance — so it is deliberately deployment-neutral. It's a
// text/template so a little live instance context (the current workspaces) can be
// rendered inline; the dynamic inputs are GuideData.
//
//go:embed guide.md
var guideTemplate string

var guideTmpl = template.Must(template.New("guide").Parse(guideTemplate))

// GuideWorkspace is one workspace as the guide lists it.
type GuideWorkspace struct {
	Name       string
	Slug       string
	ItemPrefix string
}

// GuideData is the live instance context rendered into the guide.
type GuideData struct {
	Workspaces []GuideWorkspace
}

// RenderGuide renders the conventions guide with the given live context. The
// template is static and trusted, so an execution error is a programmer bug, not
// user input — callers treat it as an internal error.
func RenderGuide(d GuideData) (string, error) {
	var b bytes.Buffer
	if err := guideTmpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// DefaultPrompts are the starter prompts seeded on first run. They are ordinary
// editable rows once seeded — an operator can reword, delete, or add to them.
// Both point agents at acta://guide so they pick up the conventions first.
var DefaultPrompts = []store.MCPPrompt{
	{
		Name:        "standup",
		Title:       "Standup",
		Description: "Summarise my open items and suggest what to pick up next.",
		Arguments: []store.MCPPromptArg{
			{Name: "workspace", Description: "Workspace slug to scope to (blank = the default board)"},
		},
		Body: `Give me a standup in Acta.

Read the conventions in the ` + "`acta://guide`" + ` resource first if you haven't.
Target workspace (blank = use list_workspaces and pick the default board): {{workspace}}

1. Call ` + "`whoami`" + `, then ` + "`list_items`" + ` filtered to me.
2. Call ` + "`list_activity`" + ` to see what's actually changed recently across the board.
3. Group my open items by status, most-recently-active first (use the activity log for recency).
4. Flag anything stale or stuck — no recent activity, or sitting in progress too long.
5. Recommend the one or two items I should pick up next, and why.

Keep it tight — this is a quick orientation, not a report.`,
	},
	{
		Name:        "triage",
		Title:       "Triage",
		Description: "Walk un-triaged items and help sort them.",
		Arguments: []store.MCPPromptArg{
			{Name: "workspace", Description: "Workspace slug to scope to (blank = the default board)"},
		},
		Body: `Help me triage the backlog in Acta.

Read the conventions in the ` + "`acta://guide`" + ` resource first if you haven't.
Target workspace (blank = use list_workspaces and pick the default board): {{workspace}}

1. List items that are unassigned or sitting in the first/"inbox" status.
2. For each, propose one of: an assignee, a status, flag as a milestone, or
   archive if it's stale or already done.
3. Show me your proposed changes as a short list and wait for my go-ahead
   before applying them with the set_* / archive tools.

Work in small batches; don't make changes I haven't confirmed.`,
	},
}
