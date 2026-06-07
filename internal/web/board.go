package web

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"html/template"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- the board page ---

type boardData struct {
	chrome
	Principal *identity.Principal
	// Board context. BoardID drives the add-lane target (data-board-id); BoardBase
	// is the current board-view path (filters/mode links hang off it); Activity
	// and Archive hrefs are this board's scoped feeds (the header toolbar).
	BoardID            string
	BoardBase          string
	ActivityHref       string
	ArchiveHref        string
	LanesDashed        bool     // this is the Backlog board — its lane/facet dots render dashed
	Mode               string   // grouping: status/milestone/release/priority/type/size/due/assignee/project
	GroupLabel         string   // the current grouping's display name (the Display-menu dropdown trigger)
	Subgroup           string   // secondary grouping axis ("" = none); splits each group into sub-sections
	SubgroupLabel      string   // the sub-grouping's display name ("None" when off)
	Order              string   // card sort within a group ("" = manual/stored position)
	OrderLabel         string   // the ordering's display name ("Manual" when off)
	ShowSubtasks       bool     // surface child items as their own cards (with a parent-ref chip)
	Layout             string   // "board" (column lanes) or "list" (stacked rows) — a display lens over the same data
	Lanes              []lane   // status mode
	Palette            []swatch // lane-colour options for the header picker
	MilestoneColumns   []milestoneColumn
	ReleaseColumns     []releaseColumn // release mode: a column per release (+ "No release")
	Columns            []groupColumn   // the "simple" groupings (priority/type/size/assignee/project/due)
	HasReleases        bool            // the workspace has ≥1 release (gates the release group option)
	StatusFilter       []statusOpt     // the status facet options
	StatusSelected     int             // count badge on the Status trigger
	Assignees          assigneeFacet   // the assignee facet (hierarchical)
	AssigneeSelected   int             // count badge on the Assignee trigger
	ProjectFilter      []projectOpt    // the project facet options (empty hides the facet)
	ProjectSelected    int             // count badge on the Project trigger
	NoProjectSelected  bool            // the "No project" token is selected
	ReleaseFilter      []releaseOpt    // the release facet options (empty hides the facet)
	ReleaseSelected    int             // count badge on the Release trigger
	NoReleaseSelected  bool            // the "No release" token is selected
	CurrentRelSelected bool            // the "Current release" (any active) token is selected
	PriorityFilter     []attrOpt       // the priority facet options (every value incl. none)
	PrioritySelected   int             // count badge on the Priority trigger
	TypeFilter         []attrOpt       // the type facet options
	TypeSelected       int             // count badge on the Type trigger
	SizeFilter         []attrOpt       // the size facet options
	SizeSelected       int             // count badge on the Size trigger
	OverdueSelected    bool            // the "Overdue" due token is selected
	FilterCount        int             // total facet selections, for the Filter button badge
	FilterActive       bool            // any facet currently narrowing the board
	Views              []viewTab       // the saved-view tab strip (seeded defaults + custom)
	ViewDirty          bool            // on a view, but the current filter differs from its stored one
	ActiveViewID       string          // the context view's id (for the "Save changes" target)
	ActiveViewName     string          // the context view's name (for the Save button title)
	ActiveViewSlug     string          // carried through the filter form so edits keep their provenance
	Modal              *modalView      // set when ?item=<id> resolves within this workspace
}

// viewTab is one tab in the board's saved-view strip. Href is the board path
// with the view's stored query; Active marks the tab whose query matches the
// current URL. ID/Slug drive the rename/delete/reorder controls.
type viewTab struct {
	ID       string
	Slug     string
	Name     string
	Icon     string
	Query    string // normalised stored query — board.js writes it to the prefs cache on click
	Href     string
	Active   bool
	Modified bool // this is the context view and the current filter has unsaved changes
}

// swatch is one option in the lane-colour picker: its hex (sent back on pick)
// and a pre-built, template-safe background declaration.
type swatch struct {
	Hex   string
	Style template.CSS
}

func palette() []swatch {
	out := make([]swatch, len(board.Palette))
	for i, hex := range board.Palette {
		out[i] = swatch{Hex: hex, Style: template.CSS("background:" + hex)}
	}
	return out
}

type lane struct {
	Status    store.Status
	Color     string
	Hidden    bool // filtered out (its status is deselected) — kept in the DOM, CSS-hidden
	Cards     []cardView
	Subgroups []cardSubgroup // set when a sub-grouping is active; the template renders these instead of Cards
}

// ColorVar is the lane's colour as a template-safe `--lane-color` declaration
// for the header dot. The value is always a palette hex (explicit or derived),
// so wrapping it as trusted CSS is safe.
func (l lane) ColorVar() template.CSS { return colorVar(l.Color) }

// milestoneColumn is one column of Milestone mode: the Backlog (ID "") or a
// root milestone (ID = its item id) holding that milestone's children.
type milestoneColumn struct {
	ID        string
	Title     string
	Color     string // the milestone's own status colour, tinting its ◆ (Backlog: "")
	Cards     []cardView
	Subgroups []cardSubgroup
}

// ColorVar is the milestone's status colour as a template-safe `--lane-color`
// declaration for its header diamond.
func (m milestoneColumn) ColorVar() template.CSS { return colorVar(m.Color) }

// releaseColumn is one column of Release mode: the "No release" bucket (ID "")
// or a release holding the items in it. Tag is the lifecycle label shown beside
// the name for non-active releases ("planned"/"shipped"), "" for active and the
// No-release bucket.
type releaseColumn struct {
	ID        string
	Name      string
	Color     string
	Tag       string
	Cards     []cardView
	Subgroups []cardSubgroup
}

// ColorVar is the release's colour as a template-safe `--lane-color` declaration
// for its header dot.
func (c releaseColumn) ColorVar() template.CSS { return colorVar(c.Color) }

type cardView struct {
	Item       store.Item
	RefID      string // human id, e.g. "ACTA-12"
	Subtasks   store.SubtaskCount
	StatusName string // the card's status name (hover tooltip / accessible label)
	Color      string // the card's lane colour, for the left bar
	Hidden     bool   // filtered out by status/assignee — kept in the DOM, CSS-hidden

	// Assignee avatar (resolved from the item's AssigneeID). HasAssignee gates
	// the meta row; AvatarText is the initials, AvatarStyle the colour.
	HasAssignee  bool
	IsAgent      bool
	AssigneeName string
	AvatarText   string
	AvatarStyle  template.CSS

	// Project chip (resolved from the item's ProjectID). HasProject gates it.
	HasProject   bool
	ProjectName  string
	ProjectColor string

	// Parent reference (only in the Show-sub-tasks view, on child cards). HasParent
	// gates the "↳ ACTA-7" chip; ParentRef is the parent's human id.
	HasParent bool
	ParentRef string

	// Release chip (resolved from the item's release membership). HasRelease gates
	// it; ReleaseID drives the filter's data attribute; ReleaseShipped dims the chip.
	HasRelease     bool
	ReleaseID      string
	ReleaseName    string
	ReleaseColor   string
	ReleaseShipped bool

	// Attribute glyphs. Each is the resolved option (slug drives the glyph/colour
	// class, label the tooltip); the template renders it only when Value != 0.
	Priority board.AttrOption
	Type     board.AttrOption
	Size     board.AttrOption
	// Due chip. HasDue gates it; DueLabel is a short human date; Overdue marks
	// past + not-done for the red styling.
	HasDue   bool
	DueLabel string
	Overdue  bool
}

// ProjectColorVar is the chip's project colour as a template-safe `--lane-color`
// declaration. The value is always a palette hex, so it's safe to emit verbatim.
func (c cardView) ProjectColorVar() template.CSS { return colorVar(c.ProjectColor) }

// ReleaseColorVar is the release chip's colour as a template-safe `--lane-color`
// declaration; like ProjectColorVar the value is always a palette hex.
func (c cardView) ReleaseColorVar() template.CSS { return colorVar(c.ReleaseColor) }

// cardRelease is a card's resolved release chip: the membership reduced to what
// the card shows. Keyed by item id in the map threaded through the card builders.
type cardRelease struct {
	ID      string
	Name    string
	Color   string
	Shipped bool
}

// buildCard assembles a card's view model, resolving its assignee (if any) to an
// avatar and its project (if any) to a chip. users maps principal id -> user for
// the avatar; projects maps project id -> project for the chip.
func buildCard(it store.Item, counts map[string]store.SubtaskCount, st store.Status, f boardFilter, users map[string]store.User, projects map[string]store.Project, releases map[string]cardRelease, prefix, doneStatusID string) cardView {
	cv := cardView{
		Item: it, RefID: refID(prefix, it.RefNum), Subtasks: counts[it.ID], StatusName: st.Name,
		Color: board.ColorFor(st), Hidden: f.cardHidden(it),
		Priority: board.Priorities.Option(it.Priority),
		Type:     board.ItemTypes.Option(it.Type),
		Size:     board.Sizes.Option(it.Size),
	}
	if it.DueDate != nil {
		done := doneStatusID != "" && it.StatusID == doneStatusID
		cv.HasDue = true
		cv.DueLabel = shortDueLabel(it.DueDate)
		cv.Overdue = board.Overdue(it.DueDate, done)
	}
	if it.AssigneeID != "" {
		if u, ok := users[it.AssigneeID]; ok {
			name := displayName(u)
			cv.HasAssignee = true
			cv.IsAgent = u.AgentOfID != ""
			cv.AssigneeName = name
			cv.AvatarText = initials(name)
			cv.AvatarStyle = avatarStyle(u.ID)
		}
	}
	if it.ProjectID != "" {
		if p, ok := projects[it.ProjectID]; ok {
			cv.HasProject = true
			cv.ProjectName = p.Name
			cv.ProjectColor = board.ProjectColorFor(p)
		}
	}
	if rc, ok := releases[it.ID]; ok {
		cv.HasRelease = true
		cv.ReleaseID = rc.ID
		cv.ReleaseName = rc.Name
		cv.ReleaseColor = rc.Color
		cv.ReleaseShipped = rc.Shipped
	}
	return cv
}

// groupLabel is the human name of a grouping mode, shown on the Display menu's
// grouping dropdown trigger. Unknown/"" falls back to Status (the default).
func groupLabel(mode string) string {
	switch mode {
	case "milestone":
		return "Milestone"
	case "release":
		return "Release"
	case "priority":
		return "Priority"
	case "type":
		return "Type"
	case "size":
		return "Size"
	case "due":
		return "Due"
	case "assignee":
		return "Assignee"
	case "project":
		return "Project"
	default:
		return "Status"
	}
}

// subgroupLabel is the human name of a sub-grouping axis for the Display menu's
// sub-group dropdown trigger; "" (off) reads as "None".
func subgroupLabel(sub string) string {
	if sub == "" {
		return "None"
	}
	return groupLabel(sub)
}

// orderLabel is the human name of a card ordering for the Display menu's Ordering
// dropdown trigger; "" (the stored drag order) reads as "Manual".
func orderLabel(order string) string {
	switch order {
	case "priority":
		return "Priority"
	case "due":
		return "Due date"
	case "title":
		return "Title"
	case "created":
		return "Created"
	default:
		return "Manual"
	}
}

// orderCards sorts a group's cards in place by the chosen ordering (stable, so
// equal keys keep their manual/position order). Manual ("") is a no-op handled by
// applyOrdering. Directions are fixed: priority urgent-first with unset last, due
// soonest-first with none last, title A–Z, created newest-first.
func orderCards(cards []cardView, order string) {
	switch order {
	case "priority":
		sort.SliceStable(cards, func(i, j int) bool {
			ap, bp := cards[i].Item.Priority, cards[j].Item.Priority
			if (ap == 0) != (bp == 0) {
				return bp == 0 // a set priority sorts before unset
			}
			return ap > bp // higher value = more urgent, first
		})
	case "due":
		sort.SliceStable(cards, func(i, j int) bool {
			ad, bd := cards[i].Item.DueDate, cards[j].Item.DueDate
			if (ad == nil) != (bd == nil) {
				return bd == nil // a dated item sorts before no-due
			}
			if ad == nil {
				return false
			}
			return ad.Before(*bd)
		})
	case "title":
		sort.SliceStable(cards, func(i, j int) bool {
			return strings.ToLower(cards[i].Item.Title) < strings.ToLower(cards[j].Item.Title)
		})
	case "created":
		sort.SliceStable(cards, func(i, j int) bool {
			return cards[i].Item.CreatedAt.After(cards[j].Item.CreatedAt)
		})
	}
}

// applyOrdering re-sorts every rendered column's cards by the chosen ordering. It
// runs after the primary columns are built and before applySubgroups, so the
// sub-sections inherit the ordering within each.
func applyOrdering(data *boardData, order string) {
	if order == "" {
		return
	}
	for i := range data.Lanes {
		orderCards(data.Lanes[i].Cards, order)
	}
	for i := range data.MilestoneColumns {
		orderCards(data.MilestoneColumns[i].Cards, order)
	}
	for i := range data.ReleaseColumns {
		orderCards(data.ReleaseColumns[i].Cards, order)
	}
	for i := range data.Columns {
		orderCards(data.Columns[i].Cards, order)
	}
}

// lastLaneID is the id of the board's final lane — the "done" equivalent used to
// decide whether a past-due item still counts as overdue. "" when there are no
// lanes (statuses must arrive in position order, as the board builds them).
func lastLaneID(statuses []store.Status) string {
	if len(statuses) == 0 {
		return ""
	}
	return statuses[len(statuses)-1].ID
}

// shortDueLabel formats a due date for a card chip: "2 Jan", with the year
// appended only when it isn't the current one.
func shortDueLabel(t *time.Time) string {
	if t == nil {
		return ""
	}
	u := t.UTC()
	if u.Year() == time.Now().UTC().Year() {
		return u.Format("2 Jan")
	}
	return u.Format("2 Jan 2006")
}

// initials takes the first letters of the first and last words (or the first two
// letters of a single word) for an avatar label.
func initials(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return "?"
	}
	if len(fields) == 1 {
		r := []rune(fields[0])
		if len(r) >= 2 {
			return strings.ToUpper(string(r[:2]))
		}
		return strings.ToUpper(string(r))
	}
	first := []rune(fields[0])
	last := []rune(fields[len(fields)-1])
	return strings.ToUpper(string(first[0]) + string(last[0]))
}

// avatarPalette is a small set of pleasant avatar gradients; a principal id
// hashes to a stable one so the same person is always the same colour.
var avatarPalette = [][2]string{
	{"#5b6cf0", "#4d7cfe"}, {"#23c3b3", "#16b8a6"}, {"#a78bff", "#8b6cf0"},
	{"#f2628c", "#e0517b"}, {"#e6a04b", "#d98a2b"}, {"#3ecf8e", "#2bb673"},
	{"#3fc7d4", "#2ba8b8"}, {"#ff8a5b", "#f26d3d"},
}

func avatarStyle(seed string) template.CSS {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	p := avatarPalette[int(h.Sum32())%len(avatarPalette)]
	return template.CSS("background:linear-gradient(145deg," + p[0] + "," + p[1] + ")")
}

// ColorVar is the card's lane colour as a template-safe `--lane-color`
// declaration driving the left bar; see lane.ColorVar.
func (c cardView) ColorVar() template.CSS { return colorVar(c.Color) }

// colorVar wraps a palette hex as a trusted `--lane-color` CSS declaration.
// Values are always palette members (explicit choices are validated server-side
// and auto colours come from the palette), so they're safe to emit verbatim.
func colorVar(hex string) template.CSS {
	if hex == "" {
		return ""
	}
	return template.CSS("--lane-color:" + hex)
}

// boardSlugParam is the board a request names: the {board} path segment (the
// board view) or, failing that, a ?board= query (the activity/archive feeds,
// which keep board off the path to avoid mux ambiguity). "" means unscoped.
func boardSlugParam(r *http.Request) string {
	if s := r.PathValue("board"); s != "" {
		return s
	}
	return r.URL.Query().Get("board")
}

// resolveBoard resolves the board a board-scoped page targets: the named board
// (path or query), or the workspace's default board when there's none. It
// writes a 404 for an unknown board slug and returns ok=false.
func (h *handlers) resolveBoard(w http.ResponseWriter, r *http.Request, ws store.Workspace) (store.Board, bool) {
	if slug := boardSlugParam(r); slug != "" {
		b, err := h.board.BoardBySlug(r.Context(), ws.ID, slug)
		if err != nil {
			http.NotFound(w, r)
			return store.Board{}, false
		}
		return b, true
	}
	b, err := h.board.DefaultBoard(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Board{}, false
	}
	return b, true
}

// boardIDFor resolves the board a board-scoped mutation targets from a request
// body's board_id, defaulting to the workspace's default board when blank. It
// confirms the board belongs to ws (a 404 otherwise) so a request can't reach
// across workspaces. Writes the error response and returns ok=false on failure.
func (h *handlers) boardIDFor(w http.ResponseWriter, r *http.Request, ws store.Workspace, boardID string) (string, bool) {
	if boardID == "" {
		bd, err := h.board.DefaultBoard(r.Context(), ws.ID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return "", false
		}
		return bd.ID, true
	}
	bd, err := h.board.BoardByID(r.Context(), boardID)
	if err != nil || bd.WorkspaceID != ws.ID {
		http.NotFound(w, r)
		return "", false
	}
	return bd.ID, true
}

// isBacklogBoard reports whether a board is the Backlog board — the v1 rule for
// the "unstarted"/dashed status treatment. Boards are dynamic internally, so
// this is the one place the special-case lives.
func isBacklogBoard(b store.Board) bool { return b.Slug == "backlog" }

// backlogBoardID is the workspace's Backlog board id, or "" if it has none. Used
// to mark activity-feed dots whose status lives on that board as dashed.
func (h *handlers) backlogBoardID(ctx context.Context, workspaceID string) string {
	if bd, err := h.board.BoardBySlug(ctx, workspaceID, "backlog"); err == nil {
		return bd.ID
	}
	return ""
}

// boardViewPath is a board's canonical view URL: the bare /{workspace} for the
// default board (position 0), /{workspace}/{board} for the rest.
func boardViewPath(wsSlug string, b store.Board) string {
	if b.Position == 0 {
		return "/" + wsSlug
	}
	return "/" + wsSlug + "/" + b.Slug
}

// itemsOnBoard keeps the items whose status belongs to the given board — an
// item's board is read off its status, never stored, so this is how a
// workspace-wide item slice is narrowed to one board.
func itemsOnBoard(items []store.Item, boardStatuses []store.Status) []store.Item {
	onBoard := make(map[string]bool, len(boardStatuses))
	for _, s := range boardStatuses {
		onBoard[s.ID] = true
	}
	out := items[:0]
	for _, it := range items {
		if onBoard[it.StatusID] {
			out = append(out, it)
		}
	}
	return out
}

// boardPage renders one of a workspace's boards: its statuses (lanes) and the
// items in each. The initial state is server-rendered so it works without
// JavaScript; board.js then layers on drag-and-drop and inline create/edit.
func (h *handlers) boardPage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	bd, ok := h.resolveBoard(w, r, ws)
	if !ok {
		return
	}
	httpx.SetWorkspaceCookie(w, ws.Slug, h.secure)

	statuses, err := h.board.BoardStatuses(r.Context(), bd.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// "Show sub-tasks" surfaces child items as their own cards; otherwise the
	// board is root-only (children show as a count on their parent).
	showSubtasks := r.URL.Query().Get("subtasks") == "1"
	var allItems []store.Item
	if showSubtasks {
		allItems, err = h.board.ItemsWithSubtasks(r.Context(), ws.ID)
	} else {
		allItems, err = h.board.Items(r.Context(), ws.ID)
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Human ref per item, so child cards can name their parent ("↳ ACTA-7").
	refByID := make(map[string]string, len(allItems))
	for _, it := range allItems {
		refByID[it.ID] = refID(ws.ItemPrefix, it.RefNum)
	}
	items := itemsOnBoard(allItems, statuses)
	ch, err := h.chromeFor(r, "home", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	doneStatusID := ""
	if len(statuses) > 0 {
		doneStatusID = statuses[len(statuses)-1].ID // "done" = the last lane
	}
	counts, err := h.board.SubtaskCounts(r.Context(), ws.ID, doneStatusID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mode := "status"
	if m := r.URL.Query().Get("mode"); board.IsGroupMode(m) {
		mode = m
	}
	// layout is a display lens orthogonal to grouping: the same lanes/items, drawn
	// as columns ("board") or stacked rows ("list"). Kept out of the saved-view
	// query (it's a personal preference, like the display props), persisted per
	// workspace by board-prefs.js.
	layout := "board"
	if r.URL.Query().Get("layout") == "list" {
		layout = "list"
	}
	// subgroup is a second grouping axis, rendered as sub-sections inside each
	// primary group. It's ignored when it would match the primary (a no-op split).
	subgroup := ""
	if s := r.URL.Query().Get("subgroup"); board.IsSubgroupMode(s) && s != mode {
		subgroup = s
	}
	// order is the sort within each group ("" = manual / stored drag position).
	order := ""
	if o := r.URL.Query().Get("order"); board.IsOrderMode(o) {
		order = o
	}

	me := principalFrom(r.Context())
	filter := newBoardFilter(r.URL.Query()["status"], r.URL.Query()["assignee"], me.ID)
	filter.projects = toSet(r.URL.Query()["project"])
	filter.releases = toSet(r.URL.Query()["release"])
	filter.priorities = toSet(r.URL.Query()["priority"])
	filter.types = toSet(r.URL.Query()["type"])
	filter.sizes = toSet(r.URL.Query()["size"])
	filter.due = toSet(r.URL.Query()["due"])
	filter.doneStatusID = doneStatusID
	users, err := h.board.Users(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Include archived projects so a card still shows its chip after the project
	// is archived; the facet (below) takes only the active ones.
	allProjects, err := h.board.Projects(r.Context(), ws.ID, true)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	projectByID := make(map[string]store.Project, len(allProjects))
	var activeProjects []store.Project
	for _, p := range allProjects {
		projectByID[p.ID] = p
		if p.ArchivedAt == nil {
			activeProjects = append(activeProjects, p)
		}
	}
	// Releases for the chips, the filter, and the facet. The chip/filter draw on
	// every release (a card keeps its chip after the release ships); the facet
	// offers only active ones. The UI keeps one release per item, so the chip map
	// reduces each item's memberships to one.
	releases, err := h.board.Releases(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	releaseByID := make(map[string]store.Release, len(releases))
	var openReleases []store.Release // planned + active — offered in the facet
	activeReleaseIDs := map[string]bool{}
	for _, rel := range releases {
		releaseByID[rel.ID] = rel
		if rel.Status != "shipped" {
			openReleases = append(openReleases, rel)
		}
		if rel.Status == "active" {
			activeReleaseIDs[rel.ID] = true // "Current release" = active only, not planned
		}
	}
	filter.activeReleases = activeReleaseIDs
	links, err := h.board.ReleaseLinks(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	releaseOf := make(map[string]string, len(links))
	releaseChips := make(map[string]cardRelease, len(links))
	for itemID, rids := range links {
		if len(rids) == 0 {
			continue
		}
		rel, ok := releaseByID[rids[0]] // one-per-item in the UI; take the first
		if !ok {
			continue
		}
		releaseOf[itemID] = rel.ID
		releaseChips[itemID] = cardRelease{ID: rel.ID, Name: rel.Name, Color: board.ReleaseColorFor(rel), Shipped: rel.Status == "shipped"}
	}
	filter.releaseOf = releaseOf
	userByID := make(map[string]store.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	// The saved-view tab strip. Each view is a stored query; the active tab is the
	// one whose normalised query matches the current URL. Release-oriented views
	// hide when the workspace lacks the releases they reference (preserving the old
	// Current Release / Releases tab visibility).
	views, err := h.board.BoardViews(r.Context(), bd.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hasReleases := len(releases) > 0
	hasActiveRelease := len(activeReleaseIDs) > 0
	currentView := board.NormalizeViewQuery(r.URL.Query())
	// The "context" view is the one you're considered to be on: the explicit
	// ?view=<slug> provenance (carried by the filter form once you start editing),
	// else the view whose stored query exactly matches the current one. Dirty means
	// you're on a view but have changed its filter without saving.
	provSlug := strings.TrimSpace(r.URL.Query().Get("view"))
	var ctxView *store.BoardView
	for i := range views {
		if provSlug != "" && views[i].Slug == provSlug {
			ctxView = &views[i]
			break
		}
	}
	if ctxView == nil {
		for i := range views {
			if views[i].Query == currentView {
				ctxView = &views[i]
				break
			}
		}
	}
	viewDirty := ctxView != nil && ctxView.Query != currentView
	viewTabs := make([]viewTab, 0, len(views))
	for i := range views {
		v := views[i]
		if board.ViewQueryHiddenByReleases(v.Query, hasReleases, hasActiveRelease) {
			continue
		}
		href := r.URL.Path
		if v.Query != "" {
			href += "?" + v.Query
		}
		active := ctxView != nil && v.ID == ctxView.ID
		viewTabs = append(viewTabs, viewTab{
			ID: v.ID, Slug: v.Slug, Name: v.Name, Icon: v.Icon, Query: v.Query,
			Href: href, Active: active, Modified: active && viewDirty,
		})
	}

	ch.ActiveBoard = bd.Slug
	data := boardData{
		chrome:             ch,
		Principal:          me,
		BoardID:            bd.ID,
		BoardBase:          r.URL.Path,
		ActivityHref:       "/" + ws.Slug + "/activity?board=" + bd.Slug,
		ArchiveHref:        "/" + ws.Slug + "/archive?board=" + bd.Slug,
		LanesDashed:        isBacklogBoard(bd),
		Mode:               mode,
		GroupLabel:         groupLabel(mode),
		Subgroup:           subgroup,
		SubgroupLabel:      subgroupLabel(subgroup),
		Order:              order,
		OrderLabel:         orderLabel(order),
		ShowSubtasks:       showSubtasks,
		Layout:             layout,
		Palette:            palette(),
		StatusFilter:       statusFacet(statuses, filter),
		StatusSelected:     len(filter.statuses),
		Assignees:          assigneeFacetFrom(users, filter),
		AssigneeSelected:   len(filter.assignees),
		ProjectFilter:      projectFacet(activeProjects, filter),
		ProjectSelected:    len(filter.projects),
		NoProjectSelected:  filter.projects["none"],
		ReleaseFilter:      releaseFacet(openReleases, filter),
		ReleaseSelected:    len(filter.releases),
		NoReleaseSelected:  filter.releases["none"],
		CurrentRelSelected: filter.releases["active"],
		PriorityFilter:     attrFacet(board.Priorities, filter.priorities),
		PrioritySelected:   len(filter.priorities),
		TypeFilter:         attrFacet(board.ItemTypes, filter.types),
		TypeSelected:       len(filter.types),
		SizeFilter:         attrFacet(board.Sizes, filter.sizes),
		SizeSelected:       len(filter.sizes),
		OverdueSelected:    filter.due["overdue"],
		FilterCount: len(filter.statuses) + len(filter.assignees) + len(filter.projects) + len(filter.releases) +
			len(filter.priorities) + len(filter.types) + len(filter.sizes) + len(filter.due),
		FilterActive: filter.active(),
		Views:        viewTabs,
		ViewDirty:    viewDirty,
		HasReleases:  hasReleases,
	}
	if ctxView != nil {
		data.ActiveViewID = ctxView.ID
		data.ActiveViewName = ctxView.Name
		data.ActiveViewSlug = ctxView.Slug
	}
	switch mode {
	case "milestone":
		cols, err := h.milestoneColumns(r.Context(), items, statuses, counts, filter, userByID, projectByID, releaseChips, ws.ItemPrefix)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		data.MilestoneColumns = cols
	case "release":
		data.ReleaseColumns = releaseColumns(items, statuses, counts, filter, userByID, projectByID, releaseChips, releaseOf, releases, ws.ItemPrefix)
	case "priority", "type", "size", "assignee", "project", "due":
		data.Columns = h.groupColumns(mode, items, statuses, counts, filter, users, userByID, activeProjects, projectByID, releaseChips, ws.ItemPrefix, doneStatusID)
	default:
		data.Lanes = groupLanes(statuses, items, counts, filter, userByID, projectByID, releaseChips, ws.ItemPrefix)
	}
	applyParentRefs(&data, refByID)
	applyOrdering(&data, order)
	applySubgroups(&data, subgroup, statuses)
	// A ?item=<id> deep link opens that item's modal (server-rendered, so it
	// works on refresh and with JS off).
	if itemID := r.URL.Query().Get("item"); itemID != "" {
		mv, found, err := h.buildModal(r, ws, itemID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if found {
			data.Modal = &mv
		}
	}
	render(w, http.StatusOK, "board.html", data)
}

// milestoneColumns builds Milestone mode: a Backlog column of root
// non-milestones, then one column per root milestone holding its children.
func (h *handlers) milestoneColumns(ctx context.Context, roots []store.Item, statuses []store.Status, counts map[string]store.SubtaskCount, filter boardFilter, users map[string]store.User, projects map[string]store.Project, releases map[string]cardRelease, prefix string) ([]milestoneColumn, error) {
	statusByID := make(map[string]store.Status, len(statuses))
	for _, s := range statuses {
		statusByID[s.ID] = s
	}
	doneStatusID := lastLaneID(statuses)
	card := func(it store.Item) cardView {
		return buildCard(it, counts, statusByID[it.StatusID], filter, users, projects, releases, prefix, doneStatusID)
	}
	// Root non-milestones gather in a leading column. It's titled "No milestone"
	// (not "Backlog") so it doesn't read as the Backlog board, which is unrelated.
	noMilestone := milestoneColumn{Title: "No milestone"}
	var milestones []store.Item
	for _, it := range roots {
		// Milestone mode is built from the parent tree (each milestone's Children),
		// so children arriving via the Show-sub-tasks fetch are skipped here to
		// avoid double-rendering — they already show under their milestone.
		if it.ParentID != "" {
			continue
		}
		if it.IsMilestone {
			milestones = append(milestones, it)
		} else {
			noMilestone.Cards = append(noMilestone.Cards, card(it))
		}
	}
	// Columns follow ms_position (their own draggable order), with creation
	// time as a stable tiebreak for any sharing a position before a reorder.
	sort.SliceStable(milestones, func(i, j int) bool {
		if milestones[i].MSPosition != milestones[j].MSPosition {
			return milestones[i].MSPosition < milestones[j].MSPosition
		}
		return milestones[i].CreatedAt.Before(milestones[j].CreatedAt)
	})
	cols := []milestoneColumn{noMilestone}
	for _, it := range milestones {
		kids, err := h.board.Children(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		col := milestoneColumn{ID: it.ID, Title: it.Title, Color: board.ColorFor(statusByID[it.StatusID])}
		for _, k := range kids {
			col.Cards = append(col.Cards, card(k))
		}
		cols = append(cols, col)
	}
	return cols, nil
}

// releaseColumns buckets items by their release for Release mode: a leading "No
// release" column, then active releases, then planned releases (both always
// shown, so they're drag targets even when empty), then any shipped release that
// still holds items on this board (so nothing is orphaned, without cluttering
// with empty history). An item's release comes from releaseOf (the join reduced
// to one, as the UI enforces); items in no release fall into the leading column.
func releaseColumns(items []store.Item, statuses []store.Status, counts map[string]store.SubtaskCount, filter boardFilter, users map[string]store.User, projects map[string]store.Project, releaseChips map[string]cardRelease, releaseOf map[string]string, releases []store.Release, prefix string) []releaseColumn {
	statusByID := make(map[string]store.Status, len(statuses))
	for _, st := range statuses {
		statusByID[st.ID] = st
	}
	doneStatusID := lastLaneID(statuses)
	byRelease := map[string][]cardView{}
	for _, it := range items {
		rid := releaseOf[it.ID]
		byRelease[rid] = append(byRelease[rid], buildCard(it, counts, statusByID[it.StatusID], filter, users, projects, releaseChips, prefix, doneStatusID))
	}
	col := func(rel store.Release, tag string) releaseColumn {
		return releaseColumn{ID: rel.ID, Name: rel.Name, Color: board.ReleaseColorFor(rel), Tag: tag, Cards: byRelease[rel.ID]}
	}
	cols := []releaseColumn{{Name: "No release", Cards: byRelease[""]}}
	for _, rel := range releases { // active first (the current focus)
		if rel.Status == "active" {
			cols = append(cols, col(rel, ""))
		}
	}
	for _, rel := range releases { // then planned (upcoming targets)
		if rel.Status == "planned" {
			cols = append(cols, col(rel, "planned"))
		}
	}
	for _, rel := range releases { // then shipped releases that still hold work here
		if rel.Status == "shipped" && len(byRelease[rel.ID]) > 0 {
			cols = append(cols, col(rel, "shipped"))
		}
	}
	return cols
}

// applyParentRefs marks each child card with its parent's human id, for the
// "↳ ACTA-7" chip in the Show-sub-tasks view. refByID maps item id → human ref.
// A no-op when no child cards are present (root cards have no ParentID). Runs
// before applySubgroups so sub-section copies inherit the fields.
func applyParentRefs(data *boardData, refByID map[string]string) {
	mark := func(cards []cardView) {
		for i := range cards {
			pid := cards[i].Item.ParentID
			if pid == "" {
				continue
			}
			if ref, ok := refByID[pid]; ok {
				cards[i].HasParent = true
				cards[i].ParentRef = ref
			}
		}
	}
	for i := range data.Lanes {
		mark(data.Lanes[i].Cards)
	}
	for i := range data.MilestoneColumns {
		mark(data.MilestoneColumns[i].Cards)
	}
	for i := range data.ReleaseColumns {
		mark(data.ReleaseColumns[i].Cards)
	}
	for i := range data.Columns {
		mark(data.Columns[i].Cards)
	}
}

// groupColumn is one column of a "simple" grouping (priority/type/size/assignee/
// project/due): items bucketed by a single key. Key is the value a card dropped
// into the column gets set to — an enum slug, a principal id, a project id, or ""
// for the none/unassigned bucket (so a drop there clears the attribute). NoDrop
// marks a bucket that can't be a drop target (the due buckets, which are too
// coarse to pin a date). The header marker is the avatar (assignee), an explicit
// hex dot (project colour), else a semantic dot keyed by Tone (styled in CSS).
type groupColumn struct {
	Key         string
	Title       string
	Tone        string // semantic dot hook (the enum/due slug), coloured in CSS
	Color       string // explicit hex dot (project colour); wins over Tone
	HasAvatar   bool   // assignee buckets show the person's avatar instead of a dot
	IsAgent     bool
	AvatarText  string
	AvatarStyle template.CSS
	NoDrop      bool
	Cards       []cardView
	Subgroups   []cardSubgroup
}

// ColorVar is the column's hex dot as a template-safe `--lane-color` declaration
// (project grouping); see lane.ColorVar.
func (c groupColumn) ColorVar() template.CSS { return colorVar(c.Color) }

// cardSubgroup is one sub-section inside a primary group: the cards sharing a
// secondary-axis value, under a small header. The header marker mirrors a primary
// column — an avatar (assignee), a hex dot (project/status colour), or a semantic
// tone dot (enum/due). Cards render with the same "card" template.
type cardSubgroup struct {
	Key         string
	Title       string
	Tone        string
	Color       string
	HasAvatar   bool
	IsAgent     bool
	AvatarText  string
	AvatarStyle template.CSS
	Cards       []cardView
}

// ColorVar is the sub-section's hex dot as a template-safe `--lane-color`
// declaration (status/project sub-grouping).
func (s cardSubgroup) ColorVar() template.CSS { return colorVar(s.Color) }

// subgroupize splits a column's cards into sub-sections by the sub axis, in a
// stable per-axis order, emitting only the sections that have cards. The header
// for each section is derived from the cards themselves (cardView already carries
// the resolved labels/colours/avatars), so no extra lookups are needed; statuses
// is consulted only to order status sub-sections by board position.
func subgroupize(cards []cardView, sub string, statuses []store.Status) []cardSubgroup {
	if sub == "" || len(cards) == 0 {
		return nil
	}
	headers := map[string]cardSubgroup{}
	byKey := map[string][]cardView{}
	for _, cv := range cards {
		h := subHeaderOf(cv, sub)
		if _, ok := headers[h.Key]; !ok {
			headers[h.Key] = h
		}
		byKey[h.Key] = append(byKey[h.Key], cv)
	}
	out := make([]cardSubgroup, 0, len(headers))
	for _, k := range subOrder(sub, headers, statuses) {
		if cs := byKey[k]; len(cs) > 0 {
			h := headers[k]
			h.Cards = cs
			out = append(out, h)
		}
	}
	return out
}

// subHeaderOf builds the sub-section header (key + marker) one card falls under
// for the given sub axis, reading the already-resolved cardView fields.
func subHeaderOf(cv cardView, sub string) cardSubgroup {
	switch sub {
	case "priority":
		return cardSubgroup{Key: cv.Priority.Slug, Title: cv.Priority.Label, Tone: cv.Priority.Slug}
	case "type":
		return cardSubgroup{Key: cv.Type.Slug, Title: cv.Type.Label, Tone: cv.Type.Slug}
	case "size":
		return cardSubgroup{Key: cv.Size.Slug, Title: cv.Size.Label, Tone: cv.Size.Slug}
	case "due":
		b := board.DueBucket(cv.Item.DueDate)
		return cardSubgroup{Key: b, Title: dueBucketTitle(b), Tone: b}
	case "status":
		return cardSubgroup{Key: cv.Item.StatusID, Title: cv.StatusName, Color: cv.Color}
	case "assignee":
		if cv.HasAssignee {
			return cardSubgroup{Key: cv.Item.AssigneeID, Title: cv.AssigneeName, HasAvatar: true, IsAgent: cv.IsAgent, AvatarText: cv.AvatarText, AvatarStyle: cv.AvatarStyle}
		}
		return cardSubgroup{Key: "", Title: "Unassigned", Tone: "none"}
	case "project":
		if cv.HasProject {
			return cardSubgroup{Key: cv.Item.ProjectID, Title: cv.ProjectName, Color: cv.ProjectColor}
		}
		return cardSubgroup{Key: "", Title: "No project", Tone: "none"}
	}
	return cardSubgroup{}
}

// subOrder returns the sub-section keys for an axis in display order: enums and
// due follow their fixed vocab order; status follows board position; assignee and
// project sort by name with the none/unassigned bucket last.
func subOrder(sub string, headers map[string]cardSubgroup, statuses []store.Status) []string {
	switch sub {
	case "priority":
		return slugOrder(board.Priorities)
	case "type":
		return slugOrder(board.ItemTypes)
	case "size":
		return slugOrder(board.Sizes)
	case "due":
		out := make([]string, len(dueBucketSpecs))
		for i, s := range dueBucketSpecs {
			out[i] = s.Key
		}
		return out
	case "status":
		out := make([]string, 0, len(statuses))
		for _, s := range statuses {
			out = append(out, s.ID)
		}
		return out
	default: // assignee, project — by title, with the none bucket ("") last
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			if (keys[i] == "") != (keys[j] == "") {
				return keys[j] == "" // empty key sorts last
			}
			return strings.ToLower(headers[keys[i]].Title) < strings.ToLower(headers[keys[j]].Title)
		})
		return keys
	}
}

func slugOrder(v board.AttrVocab) []string {
	opts := v.Options()
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Slug
	}
	return out
}

// dueBucketTitle is the display title for a due bucket key (see dueBucketSpecs).
func dueBucketTitle(key string) string {
	for _, s := range dueBucketSpecs {
		if s.Key == key {
			return s.Title
		}
	}
	return key
}

// applySubgroups fills each rendered column's Subgroups from its Cards when a
// sub-grouping is active. It runs after the primary columns are built, so it's
// uniform across every grouping mode.
func applySubgroups(data *boardData, sub string, statuses []store.Status) {
	if sub == "" {
		return
	}
	for i := range data.Lanes {
		data.Lanes[i].Subgroups = subgroupize(data.Lanes[i].Cards, sub, statuses)
	}
	for i := range data.MilestoneColumns {
		data.MilestoneColumns[i].Subgroups = subgroupize(data.MilestoneColumns[i].Cards, sub, statuses)
	}
	for i := range data.ReleaseColumns {
		data.ReleaseColumns[i].Subgroups = subgroupize(data.ReleaseColumns[i].Cards, sub, statuses)
	}
	for i := range data.Columns {
		data.Columns[i].Subgroups = subgroupize(data.Columns[i].Cards, sub, statuses)
	}
}

// groupColumns builds the columns for a "simple" grouping: bucket every item by
// the mode's key, then lay the buckets out in a stable order. buildCard renders
// each card exactly as the lane/release modes do, so filtering, live updates and
// the modal all carry over unchanged.
func (h *handlers) groupColumns(mode string, items []store.Item, statuses []store.Status, counts map[string]store.SubtaskCount, filter boardFilter, users []store.User, userByID map[string]store.User, activeProjects []store.Project, projectByID map[string]store.Project, releases map[string]cardRelease, prefix, doneStatusID string) []groupColumn {
	statusByID := make(map[string]store.Status, len(statuses))
	for _, s := range statuses {
		statusByID[s.ID] = s
	}
	card := func(it store.Item) cardView {
		return buildCard(it, counts, statusByID[it.StatusID], filter, userByID, projectByID, releases, prefix, doneStatusID)
	}
	switch mode {
	case "priority":
		return enumColumns(board.Priorities, items, card, func(it store.Item) int { return it.Priority })
	case "type":
		return enumColumns(board.ItemTypes, items, card, func(it store.Item) int { return it.Type })
	case "size":
		return enumColumns(board.Sizes, items, card, func(it store.Item) int { return it.Size })
	case "assignee":
		return assigneeColumns(items, users, card)
	case "project":
		return projectColumns(items, activeProjects, projectByID, card)
	case "due":
		return dueColumns(items, card)
	}
	return nil
}

// enumColumns lays out one column per vocabulary option (in display order, which
// is already board-friendly — Urgent first, "none" last), bucketing items by the
// option their value maps to. The column Key is the option slug, so dropping a
// card sets that value (and the "none" column clears it).
func enumColumns(vocab board.AttrVocab, items []store.Item, card func(store.Item) cardView, val func(store.Item) int) []groupColumn {
	byKey := map[string][]cardView{}
	for _, it := range items {
		slug := vocab.Slug(val(it))
		byKey[slug] = append(byKey[slug], card(it))
	}
	opts := vocab.Options()
	cols := make([]groupColumn, 0, len(opts))
	for _, o := range opts {
		cols = append(cols, groupColumn{Key: o.Slug, Title: o.Label, Tone: o.Slug, Cards: byKey[o.Slug]})
	}
	return cols
}

// assigneeColumns leads with an Unassigned bucket, then a column per principal
// (humans and their agents), so a card can be dragged onto anyone. A principal no
// longer in the workspace still gets a trailing column so its cards never vanish.
func assigneeColumns(items []store.Item, users []store.User, card func(store.Item) cardView) []groupColumn {
	byKey := map[string][]cardView{}
	for _, it := range items {
		byKey[it.AssigneeID] = append(byKey[it.AssigneeID], card(it))
	}
	cols := []groupColumn{{Key: "", Title: "Unassigned", Tone: "none", Cards: byKey[""]}}
	sorted := append([]store.User(nil), users...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(displayName(sorted[i])) < strings.ToLower(displayName(sorted[j]))
	})
	seen := map[string]bool{"": true}
	for _, u := range sorted {
		name := displayName(u)
		cols = append(cols, groupColumn{
			Key: u.ID, Title: name, HasAvatar: true, IsAgent: u.AgentOfID != "",
			AvatarText: initials(name), AvatarStyle: avatarStyle(u.ID), Cards: byKey[u.ID],
		})
		seen[u.ID] = true
	}
	cols = appendOrphanColumns(cols, byKey, seen, "Unknown", "")
	return cols
}

// projectColumns leads with a No-project bucket, then the active projects, then
// any other project still holding items here (e.g. archived) so nothing is lost.
func projectColumns(items []store.Item, active []store.Project, byID map[string]store.Project, card func(store.Item) cardView) []groupColumn {
	byKey := map[string][]cardView{}
	for _, it := range items {
		byKey[it.ProjectID] = append(byKey[it.ProjectID], card(it))
	}
	cols := []groupColumn{{Key: "", Title: "No project", Tone: "none", Cards: byKey[""]}}
	seen := map[string]bool{"": true}
	for _, p := range active {
		cols = append(cols, groupColumn{Key: p.ID, Title: p.Name, Color: board.ProjectColorFor(p), Cards: byKey[p.ID]})
		seen[p.ID] = true
	}
	// Archived (or otherwise non-active) projects that still hold items, titled and
	// tinted from the full project map; sorted for a stable order.
	var rest []string
	for key := range byKey {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		title, color := "Archived project", ""
		if p, ok := byID[key]; ok {
			title, color = p.Name, board.ProjectColorFor(p)
		}
		cols = append(cols, groupColumn{Key: key, Title: title, Color: color, Cards: byKey[key]})
	}
	return cols
}

// dueBucketSpecs is the fixed column order of due-date grouping.
var dueBucketSpecs = []struct{ Key, Title, Tone string }{
	{"overdue", "Overdue", "overdue"},
	{"today", "Today", "today"},
	{"week", "This week", "week"},
	{"later", "Later", "later"},
	{"none", "No due date", "none"},
}

// dueColumns buckets items by board.DueBucket into the fixed date columns. Every
// column is NoDrop: a bucket spans many days, so a drop can't say which date —
// the client surfaces a toast and the item keeps its date (set via the modal).
func dueColumns(items []store.Item, card func(store.Item) cardView) []groupColumn {
	byKey := map[string][]cardView{}
	for _, it := range items {
		byKey[board.DueBucket(it.DueDate)] = append(byKey[board.DueBucket(it.DueDate)], card(it))
	}
	cols := make([]groupColumn, 0, len(dueBucketSpecs))
	for _, s := range dueBucketSpecs {
		cols = append(cols, groupColumn{Key: s.Key, Title: s.Title, Tone: s.Tone, NoDrop: true, Cards: byKey[s.Key]})
	}
	return cols
}

// appendOrphanColumns appends a column for each bucket key not already laid out,
// so cards keyed to a vanished principal/project still show. Keys are sorted for
// a stable order; tone tints the placeholder dot.
func appendOrphanColumns(cols []groupColumn, byKey map[string][]cardView, seen map[string]bool, title, tone string) []groupColumn {
	var rest []string
	for key := range byKey {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		cols = append(cols, groupColumn{Key: key, Title: title, Tone: tone, Cards: byKey[key]})
	}
	return cols
}

// groupLanes buckets items under their status, attaching each item's subtask
// progress. items arrives ordered by position, so each lane stays in order.
func groupLanes(statuses []store.Status, items []store.Item, counts map[string]store.SubtaskCount, filter boardFilter, users map[string]store.User, projects map[string]store.Project, releases map[string]cardRelease, prefix string) []lane {
	byID := make(map[string]store.Status, len(statuses))
	for _, st := range statuses {
		byID[st.ID] = st
	}
	doneStatusID := lastLaneID(statuses)
	byStatus := map[string][]cardView{}
	for _, it := range items {
		byStatus[it.StatusID] = append(byStatus[it.StatusID], buildCard(it, counts, byID[it.StatusID], filter, users, projects, releases, prefix, doneStatusID))
	}
	lanes := make([]lane, len(statuses))
	for i, st := range statuses {
		lanes[i] = lane{Status: st, Color: board.ColorFor(st), Hidden: !filter.statusVisible(st.ID), Cards: byStatus[st.ID]}
	}
	return lanes
}

// --- JSON API (consumed by board.js, and by automation later) ---

type statusDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Color    string `json:"color"` // resolved display colour (never empty)
}

type itemDTO struct {
	ID       string `json:"id"`
	Ref      string `json:"ref"` // human id, e.g. "ACTA-12", so the client can paint it
	StatusID string `json:"status_id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
	Color    string `json:"color"` // resolved lane colour, so the client can paint the new card
}

// itemDTOFor builds the response for a freshly created item, resolving its lane
// colour so board.js can render the card's left bar without a reload.
func (h *handlers) itemDTOFor(ctx context.Context, it store.Item) itemDTO {
	dto := itemDTO{
		ID: it.ID, Ref: refID(h.prefixFor(ctx, it.WorkspaceID), it.RefNum),
		StatusID: it.StatusID, Title: it.Title, Position: it.Position,
	}
	if st, err := h.board.StatusByID(ctx, it.StatusID); err == nil {
		dto.Color = board.ColorFor(st)
	}
	return dto
}

func (h *handlers) statusCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		BoardID string `json:"board_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	boardID, ok := h.boardIDFor(w, r, ws, req.BoardID)
	if !ok {
		return
	}
	st, err := h.board.CreateStatus(r.Context(), boardID, req.Name)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusDTO{ID: st.ID, Name: st.Name, Position: st.Position, Color: board.ColorFor(st)})
}

func (h *handlers) statusColor(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Color string `json:"color"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetStatusColor(r.Context(), r.PathValue("id"), req.Color); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) statusRename(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.RenameStatus(r.Context(), r.PathValue("id"), req.Name); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) statusReorder(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.ReorderStatuses(r.Context(), ws.ID, req.IDs); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// milestoneReorder sets the column order of Milestone mode from the dragged
// sequence of milestone item ids (the Backlog column is fixed, so it's not in
// the list).
func (h *handlers) milestoneReorder(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.ReorderMilestones(r.Context(), ws.ID, req.IDs); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) statusDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	if err := h.board.DeleteStatus(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		StatusID string `json:"status_id"`
		Title    string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	it, err := h.board.CreateRootItemAs(r.Context(), ws.ID, req.StatusID, req.Title, principalFrom(r.Context()).ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	h.publishItemUpsert(r.Context(), clientID(r), ws.ID, it)
	writeJSON(w, http.StatusOK, h.itemDTOFor(r.Context(), it))
}

func (h *handlers) itemRename(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.RenameItem(r.Context(), r.PathValue("id"), req.Title); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.liveUpsert(r, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemMove(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		StatusID string `json:"status_id"`
		Index    int    `json:"index"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	out, err := h.board.MoveItemGated(r.Context(), r.PathValue("id"), req.StatusID, req.Index)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	// Gated and unmet: no move happened — return the checklist so the client can
	// surface it. An actual move stays a 204 (the historical contract).
	if !out.Moved {
		writeJSON(w, http.StatusOK, moveResultFrom(out))
		return
	}
	h.liveUpsert(r, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// itemSetBoard moves an item onto a board (dropping a card on a board in the
// sidebar) by giving it that board's entry lane — promote/demote. Board
// membership is derived from status, so this is a SetStatus to the entry lane;
// SetStatus handles the cross-board reposition and activity line.
func (h *handlers) itemSetBoard(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		BoardID string `json:"board_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	bd, err := h.board.BoardByID(r.Context(), req.BoardID)
	if err != nil || bd.WorkspaceID != ws.ID {
		http.NotFound(w, r)
		return
	}
	entry, err := h.board.EntryStatus(r.Context(), bd.ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	if err := h.board.SetStatus(r.Context(), r.PathValue("id"), entry.ID); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.liveUpsert(r, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemDelete(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.publishItemRemove(clientID(r), ws.ID, r.PathValue("id"))
	respond204OrRedirect(w, r, "/"+ws.Slug+"/archive")
}

// --- helpers ---

// resolveWorkspace looks up the workspace named in the {slug} path value,
// writing a 404 (or 500) and returning ok=false if it can't be served.
func (h *handlers) resolveWorkspace(w http.ResponseWriter, r *http.Request) (store.Workspace, bool) {
	ws, err := h.workspaces.BySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		http.NotFound(w, r)
		return store.Workspace{}, false
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Workspace{}, false
	}
	return ws, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeBoardErr maps a board/store error to an HTTP status plus a small JSON
// {"error": code} body the client can branch on.
func writeBoardErr(w http.ResponseWriter, err error) {
	type body struct {
		Error string `json:"error"`
	}
	switch {
	case errors.Is(err, board.ErrInvalidName):
		writeJSON(w, http.StatusBadRequest, body{"invalid_name"})
	case errors.Is(err, board.ErrInvalidTitle):
		writeJSON(w, http.StatusBadRequest, body{"invalid_title"})
	case errors.Is(err, board.ErrInvalidDescription):
		writeJSON(w, http.StatusBadRequest, body{"invalid_description"})
	case errors.Is(err, board.ErrInvalidComment):
		writeJSON(w, http.StatusBadRequest, body{"invalid_comment"})
	case errors.Is(err, board.ErrCommentForbidden):
		writeJSON(w, http.StatusForbidden, body{"comment_forbidden"})
	case errors.Is(err, store.ErrCommentNotFound):
		writeJSON(w, http.StatusNotFound, body{"comment_not_found"})
	case errors.Is(err, board.ErrInvalidDocument):
		writeJSON(w, http.StatusBadRequest, body{"invalid_document"})
	case errors.Is(err, store.ErrDocumentNotFound):
		writeJSON(w, http.StatusNotFound, body{"document_not_found"})
	case errors.Is(err, board.ErrStatusNotEmpty):
		writeJSON(w, http.StatusConflict, body{"status_not_empty"})
	case errors.Is(err, board.ErrNoStatus):
		writeJSON(w, http.StatusConflict, body{"no_status"})
	case errors.Is(err, board.ErrCycle):
		writeJSON(w, http.StatusConflict, body{"cycle"})
	case errors.Is(err, board.ErrStatusMismatch):
		writeJSON(w, http.StatusBadRequest, body{"status_mismatch"})
	case errors.Is(err, board.ErrInvalidAttribute):
		writeJSON(w, http.StatusBadRequest, body{"invalid_attribute"})
	case errors.Is(err, board.ErrProjectMismatch):
		writeJSON(w, http.StatusBadRequest, body{"project_mismatch"})
	case errors.Is(err, store.ErrProjectNotFound):
		writeJSON(w, http.StatusNotFound, body{"project_not_found"})
	case errors.Is(err, board.ErrReleaseMismatch):
		writeJSON(w, http.StatusBadRequest, body{"release_mismatch"})
	case errors.Is(err, store.ErrReleaseNotFound):
		writeJSON(w, http.StatusNotFound, body{"release_not_found"})
	case errors.Is(err, store.ErrReleaseNameTaken):
		writeJSON(w, http.StatusConflict, body{"release_name_taken"})
	case errors.Is(err, board.ErrNotMilestone):
		writeJSON(w, http.StatusBadRequest, body{"not_milestone"})
	case errors.Is(err, board.ErrInvalidColor):
		writeJSON(w, http.StatusBadRequest, body{"invalid_color"})
	case errors.Is(err, store.ErrStatusNotFound):
		writeJSON(w, http.StatusNotFound, body{"status_not_found"})
	case errors.Is(err, store.ErrBoardNotFound):
		writeJSON(w, http.StatusNotFound, body{"board_not_found"})
	case errors.Is(err, store.ErrBoardViewNotFound):
		writeJSON(w, http.StatusNotFound, body{"view_not_found"})
	case errors.Is(err, store.ErrItemNotFound):
		writeJSON(w, http.StatusNotFound, body{"item_not_found"})
	case errors.Is(err, store.ErrUserNotFound):
		writeJSON(w, http.StatusBadRequest, body{"user_not_found"})
	case errors.Is(err, board.ErrInvalidFact):
		writeJSON(w, http.StatusBadRequest, body{"invalid_fact"})
	case errors.Is(err, store.ErrFactTitleTaken):
		writeJSON(w, http.StatusConflict, body{"fact_title_taken"})
	case errors.Is(err, store.ErrFactNotFound):
		writeJSON(w, http.StatusNotFound, body{"fact_not_found"})
	case errors.Is(err, board.ErrNoPending):
		writeJSON(w, http.StatusConflict, body{"no_pending"})
	default:
		writeJSON(w, http.StatusInternalServerError, body{"internal"})
	}
}
