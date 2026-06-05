package web

import (
	"html/template"
	"sort"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// boardFilter narrows the board by status, assignee, and project. A facet with
// no selections imposes no constraint (shows all); you only narrow by selecting.
// The hierarchy/cascade in the picker is pure UI — what reaches here is a flat
// set of ids plus the tokens "me"/"unassigned" (assignees) and "none" (project).
type boardFilter struct {
	statuses  map[string]bool // selected status ids
	assignees map[string]bool // selected principal ids plus tokens "me"/"unassigned"
	projects  map[string]bool // selected project ids plus the token "none" (no project)
	me        string          // current principal id, resolving the "me" token
}

// newBoardFilter builds the status+assignee filter. The project selection is set
// separately (filter.projects = toSet(...)) so this constructor — and its test
// call sites — stay unchanged; an unset projects map imposes no constraint.
func newBoardFilter(statusSel, assigneeSel []string, me string) boardFilter {
	return boardFilter{statuses: toSet(statusSel), assignees: toSet(assigneeSel), me: me}
}

func toSet(vals []string) map[string]bool {
	if len(vals) == 0 {
		return nil
	}
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		if v != "" {
			m[v] = true
		}
	}
	return m
}

func (f boardFilter) active() bool {
	return len(f.statuses) > 0 || len(f.assignees) > 0 || len(f.projects) > 0
}

func (f boardFilter) statusVisible(id string) bool {
	return len(f.statuses) == 0 || f.statuses[id]
}

// projectVisible reports whether an item in project projectID ("" = no project)
// passes the project facet. No selections means no constraint.
func (f boardFilter) projectVisible(projectID string) bool {
	if len(f.projects) == 0 {
		return true
	}
	if projectID == "" {
		return f.projects["none"]
	}
	return f.projects[projectID]
}

func (f boardFilter) assigneeVisible(assigneeID string) bool {
	if len(f.assignees) == 0 {
		return true
	}
	if assigneeID == "" {
		return f.assignees["unassigned"]
	}
	if f.assignees[assigneeID] {
		return true
	}
	return f.assignees["me"] && assigneeID == f.me
}

// cardHidden reports whether an item is filtered out of view.
func (f boardFilter) cardHidden(it store.Item) bool {
	return !f.statusVisible(it.StatusID) || !f.assigneeVisible(it.AssigneeID) || !f.projectVisible(it.ProjectID)
}

// --- facet view models (the picker UI) ---

type statusOpt struct {
	ID       string
	Name     string
	Color    string
	Selected bool
}

func (o statusOpt) ColorVar() template.CSS { return colorVar(o.Color) }

func statusFacet(statuses []store.Status, f boardFilter) []statusOpt {
	out := make([]statusOpt, len(statuses))
	for i, s := range statuses {
		out[i] = statusOpt{ID: s.ID, Name: s.Name, Color: board.ColorFor(s), Selected: f.statuses[s.ID]}
	}
	return out
}

type projectOpt struct {
	ID       string
	Name     string
	Color    string
	Selected bool
}

func (o projectOpt) ColorVar() template.CSS { return colorVar(o.Color) }

func projectFacet(projects []store.Project, f boardFilter) []projectOpt {
	out := make([]projectOpt, len(projects))
	for i, p := range projects {
		out[i] = projectOpt{ID: p.ID, Name: p.Name, Color: board.ProjectColorFor(p), Selected: f.projects[p.ID]}
	}
	return out
}

type assigneeFacet struct {
	MeSelected         bool
	UnassignedSelected bool
	People             []personOpt
}

type personOpt struct {
	ID       string
	Display  string
	IsYou    bool
	Selected bool
	Agents   []agentOpt
}

type agentOpt struct {
	ID       string
	Display  string
	Selected bool
}

// assigneeFacetFrom builds the hierarchical assignee picker: active humans as
// parents with their active agents nested, plus the Me/Unassigned tokens.
func assigneeFacetFrom(users []store.User, f boardFilter) assigneeFacet {
	agentsByHuman := map[string][]agentOpt{}
	var humans []store.User
	for _, u := range users {
		if u.DisabledAt != nil {
			continue // active principals only
		}
		if u.AgentOfID == "" {
			humans = append(humans, u)
			continue
		}
		agentsByHuman[u.AgentOfID] = append(agentsByHuman[u.AgentOfID], agentOpt{
			ID: u.ID, Display: displayName(u), Selected: f.assignees[u.ID],
		})
	}
	sort.Slice(humans, func(i, j int) bool { return displayName(humans[i]) < displayName(humans[j]) })

	people := make([]personOpt, 0, len(humans))
	for _, h := range humans {
		ags := agentsByHuman[h.ID]
		sort.Slice(ags, func(i, j int) bool { return ags[i].Display < ags[j].Display })
		people = append(people, personOpt{
			ID:       h.ID,
			Display:  displayName(h),
			IsYou:    h.ID == f.me,
			Selected: f.assignees[h.ID],
			Agents:   ags,
		})
	}
	return assigneeFacet{
		MeSelected:         f.assignees["me"],
		UnassignedSelected: f.assignees["unassigned"],
		People:             people,
	}
}

func displayName(u store.User) string {
	if u.Display != "" {
		return u.Display
	}
	return u.Username
}
