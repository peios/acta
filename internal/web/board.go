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
	BoardID           string
	BoardBase         string
	ActivityHref      string
	ArchiveHref       string
	LanesDashed       bool     // this is the Backlog board — its lane/facet dots render dashed
	Mode              string   // "status" or "milestone"
	Lanes             []lane   // status mode
	Palette           []swatch // lane-colour options for the header picker
	MilestoneColumns  []milestoneColumn
	StatusFilter      []statusOpt   // the status facet options
	StatusSelected    int           // count badge on the Status trigger
	Assignees         assigneeFacet // the assignee facet (hierarchical)
	AssigneeSelected  int           // count badge on the Assignee trigger
	ProjectFilter     []projectOpt  // the project facet options (empty hides the facet)
	ProjectSelected   int           // count badge on the Project trigger
	NoProjectSelected bool          // the "No project" token is selected
	FilterCount       int           // status + assignee + project selections, for the Filter button badge
	FilterActive      bool          // any facet currently narrowing the board
	ViewMine          bool          // the active view is "assigned to me" (My items tab)
	Modal             *modalView    // set when ?item=<id> resolves within this workspace
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
	Status store.Status
	Color  string
	Hidden bool // filtered out (its status is deselected) — kept in the DOM, CSS-hidden
	Cards  []cardView
}

// ColorVar is the lane's colour as a template-safe `--lane-color` declaration
// for the header dot. The value is always a palette hex (explicit or derived),
// so wrapping it as trusted CSS is safe.
func (l lane) ColorVar() template.CSS { return colorVar(l.Color) }

// milestoneColumn is one column of Milestone mode: the Backlog (ID "") or a
// root milestone (ID = its item id) holding that milestone's children.
type milestoneColumn struct {
	ID    string
	Title string
	Color string // the milestone's own status colour, tinting its ◆ (Backlog: "")
	Cards []cardView
}

// ColorVar is the milestone's status colour as a template-safe `--lane-color`
// declaration for its header diamond.
func (m milestoneColumn) ColorVar() template.CSS { return colorVar(m.Color) }

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
}

// ProjectColorVar is the chip's project colour as a template-safe `--lane-color`
// declaration. The value is always a palette hex, so it's safe to emit verbatim.
func (c cardView) ProjectColorVar() template.CSS { return colorVar(c.ProjectColor) }

// buildCard assembles a card's view model, resolving its assignee (if any) to an
// avatar and its project (if any) to a chip. users maps principal id -> user for
// the avatar; projects maps project id -> project for the chip.
func buildCard(it store.Item, counts map[string]store.SubtaskCount, st store.Status, f boardFilter, users map[string]store.User, projects map[string]store.Project, prefix string) cardView {
	cv := cardView{
		Item: it, RefID: refID(prefix, it.RefNum), Subtasks: counts[it.ID], StatusName: st.Name,
		Color: board.ColorFor(st), Hidden: f.cardHidden(it),
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
	return cv
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
	allItems, err := h.board.Items(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
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
	if r.URL.Query().Get("mode") == "milestone" {
		mode = "milestone"
	}

	me := principalFrom(r.Context())
	filter := newBoardFilter(r.URL.Query()["status"], r.URL.Query()["assignee"], me.ID)
	filter.projects = toSet(r.URL.Query()["project"])
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
	userByID := make(map[string]store.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	ch.ActiveBoard = bd.Slug
	data := boardData{
		chrome:            ch,
		Principal:         me,
		BoardID:           bd.ID,
		BoardBase:         r.URL.Path,
		ActivityHref:      "/" + ws.Slug + "/activity?board=" + bd.Slug,
		ArchiveHref:       "/" + ws.Slug + "/archive?board=" + bd.Slug,
		LanesDashed:       isBacklogBoard(bd),
		Mode:              mode,
		Palette:           palette(),
		StatusFilter:      statusFacet(statuses, filter),
		StatusSelected:    len(filter.statuses),
		Assignees:         assigneeFacetFrom(users, filter),
		AssigneeSelected:  len(filter.assignees),
		ProjectFilter:     projectFacet(activeProjects, filter),
		ProjectSelected:   len(filter.projects),
		NoProjectSelected: filter.projects["none"],
		FilterCount:       len(filter.statuses) + len(filter.assignees) + len(filter.projects),
		FilterActive:      filter.active(),
		ViewMine:          filter.assignees["me"] && len(filter.assignees) == 1 && len(filter.statuses) == 0,
	}
	if mode == "milestone" {
		cols, err := h.milestoneColumns(r.Context(), items, statuses, counts, filter, userByID, projectByID, ws.ItemPrefix)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		data.MilestoneColumns = cols
	} else {
		data.Lanes = groupLanes(statuses, items, counts, filter, userByID, projectByID, ws.ItemPrefix)
	}
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
func (h *handlers) milestoneColumns(ctx context.Context, roots []store.Item, statuses []store.Status, counts map[string]store.SubtaskCount, filter boardFilter, users map[string]store.User, projects map[string]store.Project, prefix string) ([]milestoneColumn, error) {
	statusByID := make(map[string]store.Status, len(statuses))
	for _, s := range statuses {
		statusByID[s.ID] = s
	}
	card := func(it store.Item) cardView {
		return buildCard(it, counts, statusByID[it.StatusID], filter, users, projects, prefix)
	}
	// Root non-milestones gather in a leading column. It's titled "No milestone"
	// (not "Backlog") so it doesn't read as the Backlog board, which is unrelated.
	noMilestone := milestoneColumn{Title: "No milestone"}
	var milestones []store.Item
	for _, it := range roots {
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

// groupLanes buckets items under their status, attaching each item's subtask
// progress. items arrives ordered by position, so each lane stays in order.
func groupLanes(statuses []store.Status, items []store.Item, counts map[string]store.SubtaskCount, filter boardFilter, users map[string]store.User, projects map[string]store.Project, prefix string) []lane {
	byID := make(map[string]store.Status, len(statuses))
	for _, st := range statuses {
		byID[st.ID] = st
	}
	byStatus := map[string][]cardView{}
	for _, it := range items {
		byStatus[it.StatusID] = append(byStatus[it.StatusID], buildCard(it, counts, byID[it.StatusID], filter, users, projects, prefix))
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
	if err := h.board.MoveItem(r.Context(), r.PathValue("id"), req.StatusID, req.Index); err != nil {
		writeBoardErr(w, err)
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
	case errors.Is(err, board.ErrStatusNotEmpty):
		writeJSON(w, http.StatusConflict, body{"status_not_empty"})
	case errors.Is(err, board.ErrNoStatus):
		writeJSON(w, http.StatusConflict, body{"no_status"})
	case errors.Is(err, board.ErrCycle):
		writeJSON(w, http.StatusConflict, body{"cycle"})
	case errors.Is(err, board.ErrStatusMismatch):
		writeJSON(w, http.StatusBadRequest, body{"status_mismatch"})
	case errors.Is(err, board.ErrProjectMismatch):
		writeJSON(w, http.StatusBadRequest, body{"project_mismatch"})
	case errors.Is(err, store.ErrProjectNotFound):
		writeJSON(w, http.StatusNotFound, body{"project_not_found"})
	case errors.Is(err, board.ErrInvalidColor):
		writeJSON(w, http.StatusBadRequest, body{"invalid_color"})
	case errors.Is(err, store.ErrStatusNotFound):
		writeJSON(w, http.StatusNotFound, body{"status_not_found"})
	case errors.Is(err, store.ErrItemNotFound):
		writeJSON(w, http.StatusNotFound, body{"item_not_found"})
	case errors.Is(err, store.ErrUserNotFound):
		writeJSON(w, http.StatusBadRequest, body{"user_not_found"})
	default:
		writeJSON(w, http.StatusInternalServerError, body{"internal"})
	}
}
