package web

import (
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- releases overview ---

type releasesData struct {
	chrome
	Principal *identity.Principal
	Active    []releaseRow
	Planned   []releaseRow
	Shipped   []releaseRow
	Err       string
}

// releaseRow is one release on the overview: its lifecycle, when it shipped or
// is due, size-weighted progress with a thumbnail of how it got there, and the
// resolved colour driving its accent dot.
type releaseRow struct {
	ID          string
	Name        string
	Color       string
	Status      string // planned|active|shipped — drives the badge
	ShippedWhen string // formatted shipped_at, "" unless shipped
	HasDesc     bool
	Done        int
	Total       int
	Pct         int
	Href        string
	// TargetWhen is the target date, and Track a one-word verdict on it
	// ("on track", "3d late", "overdue") — "" when there's no target or no pace
	// to judge it by.
	TargetWhen string
	Track      string
	Late       bool
	Spark      sparkline
	HasSpark   bool
}

func (r releaseRow) ColorVar() template.CSS { return colorVar(r.Color) }

// releases dispatches GET /{slug}/releases: a ?r=<id> query opens that single
// release, otherwise it's the overview. (A query rather than a path segment,
// like projects, to dodge the mux's wildcard-ambiguity minefield.)
func (h *handlers) releases(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("r") != "" {
		h.releasePage(w, r)
		return
	}
	h.releasesOverview(w, r)
}

func (h *handlers) releasesOverview(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	ch, err := h.chromeFor(r, "releases", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	releases, err := h.board.Releases(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Keep today's history point current before reading any of it back.
	if err := h.board.EnsureSnapshot(r.Context(), ws.ID); err != nil {
		logSnapshotErr(r, err)
	}
	progress, err := h.board.ReleaseProgress(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ids := make([]string, 0, len(releases))
	for _, rel := range releases {
		ids = append(ids, rel.ID)
	}
	now := time.Now()
	history, err := h.board.ProgressHistories(r.Context(), board.SubjectRelease, ids, now.Add(-historyWindow))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var active, planned, shipped []releaseRow
	for _, rel := range releases {
		c := progress[rel.ID]
		hist := history[rel.ID]
		f := board.Project(hist, c, rel.TargetDate, now)
		row := releaseRow{
			ID: rel.ID, Name: rel.Name, Color: board.ReleaseColorFor(rel),
			Status: rel.Status, HasDesc: rel.Description != "",
			Done: c.DoneItems, Total: c.TotalItems, Pct: c.Pct(),
			Href: "/" + ws.Slug + "/releases?r=" + rel.ID,
		}
		if rel.TargetDate != nil {
			row.TargetWhen = formatDay(*rel.TargetDate)
			row.Track, row.Late = trackVerdict(f, rel.Status, now)
		}
		row.Spark, row.HasSpark = buildSparkline(hist)
		if rel.ShippedAt != nil {
			row.ShippedWhen = formatWhen(*rel.ShippedAt)
		}
		switch rel.Status {
		case "shipped":
			shipped = append(shipped, row)
		case "planned":
			planned = append(planned, row)
		default:
			active = append(active, row)
		}
	}
	// Shipped releases read as a changelog: most recently shipped first.
	sort.SliceStable(shipped, func(i, j int) bool { return shipped[i].ShippedWhen > shipped[j].ShippedWhen })
	render(w, http.StatusOK, "releases.html", releasesData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Active:    active,
		Planned:   planned,
		Shipped:   shipped,
		Err:       releaseError(r.URL.Query().Get("err")),
	})
}

// --- single release ---

type releasePageData struct {
	chrome
	Principal   *identity.Principal
	Release     store.Release
	Color       string
	DescHTML    template.HTML
	HasDesc     bool
	Status      string // planned|active|shipped — drives the badge and the transition buttons
	ShippedWhen string
	Done        int
	Total       int
	Pct         int
	DonePoints  int
	TotalPoints int
	Items       []releaseItemRow
	Err         string

	// Target date: TargetValue feeds the edit form's date input (YYYY-MM-DD),
	// TargetWhen is the human form, and Track/Late are the verdict on it.
	TargetValue string
	TargetWhen  string
	HasTarget   bool
	Track       string
	Late        bool

	// The burn-up and what the pace implies. Pace/ETA are "" when there's not
	// enough history to say anything honest.
	Chart        burnup
	HasChart     bool
	HasSynthetic bool
	Pace         string
	ETAWhen      string
	Moves        []releaseMoveRow
}

// releaseMoveRow is one line of the "what moved" digest.
type releaseMoveRow struct {
	Kind  string // done|reopened|added
	Title string
	Href  string
	When  string
}

func (d releasePageData) ColorVar() template.CSS { return colorVar(d.Color) }

// releaseItemRow is one of a release's items on its page: a deep link to the
// item (opening its modal on the board), with a status chip and assignee avatar.
type releaseItemRow struct {
	RefID        string
	Title        string
	StatusName   string
	StatusColor  string
	Href         string
	HasAssignee  bool
	IsAgent      bool
	AssigneeName string
	AvatarText   string
	AvatarStyle  template.CSS
}

func (r releaseItemRow) StatusColorVar() template.CSS { return colorVar(r.StatusColor) }

func (h *handlers) releasePage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	rel, err := h.board.Release(r.Context(), r.URL.Query().Get("r"))
	if errors.Is(err, store.ErrReleaseNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if rel.WorkspaceID != ws.ID {
		http.NotFound(w, r)
		return
	}
	ch, err := h.chromeFor(r, "releases", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	items, err := h.board.ReleaseItems(r.Context(), rel.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	statuses, err := h.board.Statuses(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	statusByID := make(map[string]store.Status, len(statuses))
	for _, s := range statuses {
		statusByID[s.ID] = s
	}
	users, err := h.board.Users(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	userByID := make(map[string]store.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	doneIDs := map[string]bool{}
	for _, b := range ch.Boards { // last lane of each board = "done"
		if lanes, lerr := h.board.BoardStatuses(r.Context(), b.ID); lerr == nil && len(lanes) > 0 {
			doneIDs[lanes[len(lanes)-1].ID] = true
		}
	}
	done := 0
	rows := make([]releaseItemRow, 0, len(items))
	for _, it := range items {
		st := statusByID[it.StatusID]
		if doneIDs[it.StatusID] {
			done++
		}
		row := releaseItemRow{
			RefID: refID(ws.ItemPrefix, it.RefNum), Title: it.Title,
			StatusName: st.Name, StatusColor: board.ColorFor(st),
			Href: "/" + ws.Slug + "?item=" + it.ID,
		}
		if u, ok := userByID[it.AssigneeID]; ok {
			name := displayName(u)
			row.HasAssignee, row.IsAgent = true, u.AgentOfID != ""
			row.AssigneeName, row.AvatarText, row.AvatarStyle = name, initials(name), avatarStyle(u.ID)
		}
		rows = append(rows, row)
	}
	shippedWhen := ""
	if rel.ShippedAt != nil {
		shippedWhen = formatWhen(*rel.ShippedAt)
	}

	// Progress, history and what they imply. The page still renders if any of
	// this is missing — a release with no history is just a release with no
	// chart yet.
	if err := h.board.EnsureSnapshot(r.Context(), ws.ID); err != nil {
		logSnapshotErr(r, err)
	}
	now := time.Now()
	progress, err := h.board.ReleaseProgress(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	cur := progress[rel.ID]
	hist, err := h.board.ProgressHistory(r.Context(), board.SubjectRelease, rel.ID, now.Add(-historyWindow))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	f := board.Project(hist, cur, rel.TargetDate, now)
	moves, err := h.board.ReleaseMoves(r.Context(), rel.ID, now.Add(-digestWindow), digestLimit)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	moveRows := make([]releaseMoveRow, 0, len(moves))
	for _, m := range moves {
		moveRows = append(moveRows, releaseMoveRow{
			Kind: m.Kind, Title: m.Title, When: relativeWhen(m.At),
			Href: "/" + ws.Slug + "?item=" + m.ItemID,
		})
	}

	data := releasePageData{
		chrome:      ch,
		Principal:   principalFrom(r.Context()),
		Release:     rel,
		Color:       board.ReleaseColorFor(rel),
		DescHTML:    mdToHTML(rel.Description),
		HasDesc:     rel.Description != "",
		Status:      rel.Status,
		ShippedWhen: shippedWhen,
		Done:        done,
		Total:       len(items),
		Pct:         cur.Pct(),
		DonePoints:  cur.DonePoints,
		TotalPoints: cur.TotalPoints,
		Items:       rows,
		Moves:       moveRows,
		Err:         releaseError(r.URL.Query().Get("err")),
	}
	if rel.TargetDate != nil {
		data.HasTarget = true
		data.TargetValue = board.DueString(rel.TargetDate)
		data.TargetWhen = formatDay(*rel.TargetDate)
		data.Track, data.Late = trackVerdict(f, rel.Status, now)
	}
	if f.HasPace {
		data.Pace = formatPace(f.PointsPerDay)
	}
	if f.ETA != nil {
		data.ETAWhen = formatDay(*f.ETA)
	}
	data.Chart, data.HasChart = buildBurnup(hist, cur, f, now)
	data.HasSynthetic = data.Chart.HasSynthetic
	render(w, http.StatusOK, "release.html", data)
}

// --- release mutations (form posts) ---

func (h *handlers) releaseCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	target, err := board.ParseDue(r.PostFormValue("target_date"))
	if err != nil {
		http.Redirect(w, r, "/"+ws.Slug+"/releases?err=invalid_target", http.StatusSeeOther)
		return
	}
	rel, err := h.board.CreateRelease(r.Context(), ws.ID,
		r.PostFormValue("name"), r.PostFormValue("description"), r.PostFormValue("status"),
		target, principalFrom(r.Context()).ID)
	if err != nil {
		redirectReleaseErr(w, r, "/"+ws.Slug+"/releases", err)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/releases?r="+rel.ID, http.StatusSeeOther)
}

func (h *handlers) releaseUpdate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	target, err := board.ParseDue(r.PostFormValue("target_date"))
	if err != nil {
		http.Redirect(w, r, "/"+ws.Slug+"/releases?r="+id+"&err=invalid_target", http.StatusSeeOther)
		return
	}
	err = h.board.UpdateRelease(r.Context(), id, r.PostFormValue("name"), r.PostFormValue("description"), target)
	if errors.Is(err, store.ErrReleaseNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		redirectReleaseErr(w, r, "/"+ws.Slug+"/releases?r="+id, err)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/releases?r="+id, http.StatusSeeOther)
}

// releaseSetStatus moves a release along its lifecycle (planned → active →
// shipped, and back) from the transition buttons on its page.
func (h *handlers) releaseSetStatus(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	err := h.board.SetReleaseStatus(r.Context(), id, r.PostFormValue("status"))
	if errors.Is(err, store.ErrReleaseNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		redirectReleaseErr(w, r, "/"+ws.Slug+"/releases?r="+id, err)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/releases?r="+id, http.StatusSeeOther)
}

func (h *handlers) releaseDelete(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.DeleteRelease(r.Context(), r.PathValue("id")); err != nil && !errors.Is(err, store.ErrReleaseNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/releases", http.StatusSeeOther)
}

// itemSetRelease puts an item in a release (or clears it), from the modal's
// release picker. JSON like the other item-field mutations.
func (h *handlers) itemSetRelease(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		ReleaseID string `json:"release_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetItemRelease(r.Context(), r.PathValue("id"), req.ReleaseID); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.liveUpsert(r, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// itemConvertToRelease converts a milestone into a release (from the item
// modal's overflow menu) and returns the new release's URL as JSON, so the
// client can navigate to it.
func (h *handlers) itemConvertToRelease(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	rel, err := h.board.ConvertMilestoneToRelease(r.Context(), r.PathValue("id"), principalFrom(r.Context()).ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/" + ws.Slug + "/releases?r=" + rel.ID})
}

// redirectReleaseErr bounces back to back with a known error code; an unknown
// error is a 500.
func redirectReleaseErr(w http.ResponseWriter, r *http.Request, back string, err error) {
	var code string
	switch {
	case errors.Is(err, board.ErrInvalidReleaseName):
		code = "invalid_name"
	case errors.Is(err, board.ErrInvalidReleaseDesc):
		code = "invalid_desc"
	case errors.Is(err, board.ErrInvalidReleaseStatus):
		code = "invalid_status"
	case errors.Is(err, store.ErrReleaseNameTaken):
		code = "name_taken"
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	http.Redirect(w, r, back+sep+"err="+code, http.StatusSeeOther)
}

func releaseError(code string) string {
	switch code {
	case "invalid_name":
		return "Enter a release name (1–80 characters)."
	case "invalid_desc":
		return "That description is too long."
	case "invalid_status":
		return "Pick a valid release status."
	case "name_taken":
		return "A release with that name already exists in this workspace."
	case "invalid_target":
		return "Enter the target date as YYYY-MM-DD, or leave it blank."
	default:
		return ""
	}
}

// --- progress presentation ---

const (
	// digestWindow is how far back the "what moved" list looks, and digestLimit
	// caps it: a glance at the last fortnight, not a second activity log.
	digestWindow = 14 * 24 * time.Hour
	digestLimit  = 8
)

// trackVerdict turns a forecast into the short badge shown against a target
// date, and whether it should read as a warning. A shipped release is judged on
// what happened, not on what might; a release with no measurable pace gets no
// verdict at all rather than a falsely confident one.
func trackVerdict(f board.Forecast, status string, now time.Time) (string, bool) {
	if !f.HasTarget || status == "shipped" {
		return "", false
	}
	if f.Done {
		return "complete", false
	}
	if today := truncDay(now); today.After(f.Target) {
		return fmt.Sprintf("%s overdue", plural(int(today.Sub(f.Target).Hours()/24), "day")), true
	}
	if !f.HasPace || f.ETA == nil {
		return "", false
	}
	if f.DaysLate > 0 {
		return fmt.Sprintf("%s late", plural(f.DaysLate, "day")), true
	}
	return "on track", false
}

// formatDay renders a date the way a target reads in a sentence ("14 Oct").
func formatDay(t time.Time) string { return t.UTC().Format("2 Jan") }

// formatPace renders velocity as points per week, which is the unit people
// actually plan in — "0.4 points a day" means nothing to anyone.
func formatPace(pointsPerDay float64) string {
	return fmt.Sprintf("%.1f pts/week", pointsPerDay*7)
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// logSnapshotErr notes a failed write-on-read measurement. Recording history is
// a side-effect of viewing a page: it must never take the page down with it.
func logSnapshotErr(r *http.Request, err error) {
	slog.WarnContext(r.Context(), "progress snapshot failed", "err", err)
}
