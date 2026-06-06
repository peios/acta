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
	// releases holds selected release ids plus two tokens: "none" (in no release)
	// and "active" (in any active release — the "Current release" convenience).
	releases       map[string]bool
	releaseOf      map[string]string // item id -> its release id (single, as the UI enforces), for releaseVisible
	activeReleases map[string]bool   // ids of active (non-shipped) releases, resolving the "active" token
	me             string            // current principal id, resolving the "me" token
	// Attribute facets: selected priority/type/size slugs (incl. "none" for unset).
	priorities map[string]bool
	types      map[string]bool
	sizes      map[string]bool
	// due holds the token "overdue" (past + not-done). doneStatusID is the board's
	// last lane, resolving done-ness for the overdue check.
	due          map[string]bool
	doneStatusID string
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
	return len(f.statuses) > 0 || len(f.assignees) > 0 || len(f.projects) > 0 || len(f.releases) > 0 ||
		len(f.priorities) > 0 || len(f.types) > 0 || len(f.sizes) > 0 || len(f.due) > 0
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

// releaseVisible reports whether an item passes the release facet, looking its
// release up by item id (releases live in a join, not on the item). No
// selections means no constraint; the "none" token matches items in no release,
// and the "active" token ("Current release") matches items in any active release.
func (f boardFilter) releaseVisible(itemID string) bool {
	if len(f.releases) == 0 {
		return true
	}
	rid := f.releaseOf[itemID]
	if rid == "" {
		return f.releases["none"]
	}
	if f.releases[rid] {
		return true
	}
	return f.releases["active"] && f.activeReleases[rid]
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

// enumVisible reports whether an item whose attribute has the given slug passes
// an enum facet (priority/type/size). No selections means no constraint.
func enumVisible(sel map[string]bool, slug string) bool {
	return len(sel) == 0 || sel[slug]
}

// dueVisible reports whether an item passes the due facet. The only token is
// "overdue" (past + not in the board's last lane); no selections means no
// constraint.
func (f boardFilter) dueVisible(it store.Item) bool {
	if len(f.due) == 0 {
		return true
	}
	if f.due["overdue"] {
		return board.Overdue(it.DueDate, it.StatusID == f.doneStatusID)
	}
	return true
}

// cardHidden reports whether an item is filtered out of view.
func (f boardFilter) cardHidden(it store.Item) bool {
	return !f.statusVisible(it.StatusID) || !f.assigneeVisible(it.AssigneeID) ||
		!f.projectVisible(it.ProjectID) || !f.releaseVisible(it.ID) ||
		!enumVisible(f.priorities, board.Priorities.Slug(it.Priority)) ||
		!enumVisible(f.types, board.ItemTypes.Slug(it.Type)) ||
		!enumVisible(f.sizes, board.Sizes.Slug(it.Size)) ||
		!f.dueVisible(it)
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

type releaseOpt struct {
	ID       string
	Name     string
	Color    string
	Selected bool
}

func (o releaseOpt) ColorVar() template.CSS { return colorVar(o.Color) }

func releaseFacet(releases []store.Release, f boardFilter) []releaseOpt {
	out := make([]releaseOpt, len(releases))
	for i, r := range releases {
		out[i] = releaseOpt{ID: r.ID, Name: r.Name, Color: board.ReleaseColorFor(r), Selected: f.releases[r.ID]}
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

// attrOpt is one option in a priority/type/size filter facet (every value of the
// vocabulary, including the "none"/unset option so you can find unset items).
type attrOpt struct {
	Slug     string
	Label    string
	Selected bool
}

func attrFacet(v board.AttrVocab, sel map[string]bool) []attrOpt {
	opts := v.Options()
	out := make([]attrOpt, len(opts))
	for i, o := range opts {
		out[i] = attrOpt{Slug: o.Slug, Label: o.Label, Selected: sel[o.Slug]}
	}
	return out
}
