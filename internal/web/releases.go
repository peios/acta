package web

import (
	"errors"
	"html/template"
	"net/http"
	"sort"
	"strings"

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

// releaseRow is one release on the overview: its lifecycle, when it shipped, and
// top-level item progress, plus the resolved colour driving its accent dot.
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
	progress, err := h.board.ReleaseProgress(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var active, planned, shipped []releaseRow
	for _, rel := range releases {
		c := progress[rel.ID]
		row := releaseRow{
			ID: rel.ID, Name: rel.Name, Color: board.ReleaseColorFor(rel),
			Status: rel.Status, HasDesc: rel.Description != "",
			Done: c.Done, Total: c.Total, Pct: pct(c.Done, c.Total),
			Href: "/" + ws.Slug + "/releases?r=" + rel.ID,
		}
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
	Items       []releaseItemRow
	Err         string
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
	render(w, http.StatusOK, "release.html", releasePageData{
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
		Pct:         pct(done, len(items)),
		Items:       rows,
		Err:         releaseError(r.URL.Query().Get("err")),
	})
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
	rel, err := h.board.CreateRelease(r.Context(), ws.ID,
		r.PostFormValue("name"), r.PostFormValue("description"), r.PostFormValue("status"), principalFrom(r.Context()).ID)
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
	err := h.board.UpdateRelease(r.Context(), id, r.PostFormValue("name"), r.PostFormValue("description"))
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
	default:
		return ""
	}
}
