package web

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- projects overview ---

type projectsData struct {
	chrome
	Principal *identity.Principal
	Projects  []projectRow
	Form      projectFormOpts
	Err       string
}

// projectRow is one project on the overview: its lead, lifecycle status, and
// top-level item progress, plus the resolved colour driving its accent dot.
type projectRow struct {
	Slug      string
	Name      string
	Status    string
	Color     string
	HasBrief  bool
	Lead      string
	LeadAgent bool
	Done      int
	Total     int
	Pct       int
	Href      string
}

func (p projectRow) ColorVar() template.CSS { return colorVar(p.Color) }

// projectFormOpts are the option lists a create/edit project form renders from.
type projectFormOpts struct {
	Leads    []store.User
	Statuses []string
	Palette  []swatch
}

// projects dispatches GET /{slug}/projects: a ?p=<slug> query opens that single
// project, otherwise it's the overview. (A query rather than a path segment to
// keep the route out of the mux's wildcard-ambiguity minefield — see server.go.)
func (h *handlers) projects(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("p") != "" {
		h.projectPage(w, r)
		return
	}
	h.projectsOverview(w, r)
}

func (h *handlers) projectsOverview(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	ch, err := h.chromeFor(r, "projects", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	projects, err := h.board.Projects(r.Context(), ws.ID, false)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	progress, err := h.board.ProjectProgress(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
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
	rows := make([]projectRow, 0, len(projects))
	for _, p := range projects {
		c := progress[p.ID]
		lead, leadAgent := "", false
		if u, ok := userByID[p.LeadID]; ok {
			lead, leadAgent = displayName(u), u.AgentOfID != ""
		}
		rows = append(rows, projectRow{
			Slug: p.Slug, Name: p.Name, Status: p.Status, Color: board.ProjectColorFor(p),
			HasBrief: p.Brief != "", Lead: lead, LeadAgent: leadAgent,
			Done: c.DoneItems, Total: c.TotalItems, Pct: c.Pct(),
			Href: "/" + ws.Slug + "/projects?p=" + p.Slug,
		})
	}
	leads, err := h.board.Assignables(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "projects.html", projectsData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Projects:  rows,
		Form:      projectFormOpts{Leads: leads, Statuses: board.ProjectStatuses, Palette: palette()},
		Err:       projectError(r.URL.Query().Get("err")),
	})
}

// --- single project ---

type projectPageData struct {
	chrome
	Principal *identity.Principal
	Project   store.Project
	Color     string
	BriefHTML template.HTML
	HasBrief  bool
	Lead      string
	LeadAgent bool
	Done      int
	Total     int
	Pct       int
	Items     []projectItemRow
	Archived  bool
	Watching  bool
	WatchCats []catToggle
	Form      projectFormOpts
	Err       string
}

func (d projectPageData) ColorVar() template.CSS { return colorVar(d.Color) }

// projectItemRow is one of a project's items on its page: a deep link to the
// item (opening its modal on the board), with a status chip and assignee avatar.
type projectItemRow struct {
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

func (r projectItemRow) StatusColorVar() template.CSS { return colorVar(r.StatusColor) }

func (h *handlers) projectPage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	p, err := h.board.ProjectBySlug(r.Context(), ws.ID, r.URL.Query().Get("p"))
	if errors.Is(err, store.ErrProjectNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ch, err := h.chromeFor(r, "projects", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	items, err := h.board.ProjectItems(r.Context(), p.ID)
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
	rows := make([]projectItemRow, 0, len(items))
	for _, it := range items {
		st := statusByID[it.StatusID]
		if doneIDs[it.StatusID] {
			done++
		}
		row := projectItemRow{
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
	lead, leadAgent := "", false
	if u, ok := userByID[p.LeadID]; ok {
		lead, leadAgent = displayName(u), u.AgentOfID != ""
	}
	leads, err := h.board.Assignables(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// The Watch control reflects the viewer's project subscription: whether they
	// watch it and which categories its filter delivers (for the dropdown).
	watchSub, watching, _ := h.board.SubscriptionFor(r.Context(), principalID(r), store.SubjectProject, p.ID)
	render(w, http.StatusOK, "project.html", projectPageData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Project:   p,
		Color:     board.ProjectColorFor(p),
		BriefHTML: mdToHTML(p.Brief),
		HasBrief:  p.Brief != "",
		Lead:      lead,
		LeadAgent: leadAgent,
		Done:      done,
		Total:     len(items),
		Pct:       pct(done, len(items)),
		Items:     rows,
		Archived:  p.ArchivedAt != nil,
		Watching:  watching,
		WatchCats: catToggles(watchSub.Events),
		Form:      projectFormOpts{Leads: leads, Statuses: board.ProjectStatuses, Palette: palette()},
		Err:       projectError(r.URL.Query().Get("err")),
	})
}

// --- project mutations (form posts) ---

func (h *handlers) projectCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p, err := h.board.CreateProject(r.Context(), ws.ID,
		r.PostFormValue("name"), r.PostFormValue("brief"), r.PostFormValue("lead_id"),
		r.PostFormValue("status"), r.PostFormValue("color"), principalFrom(r.Context()).ID)
	if err != nil {
		redirectProjectErr(w, r, "/"+ws.Slug+"/projects", err)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/projects?p="+p.Slug, http.StatusSeeOther)
}

func (h *handlers) projectUpdate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	err := h.board.UpdateProject(r.Context(), id,
		r.PostFormValue("name"), r.PostFormValue("brief"), r.PostFormValue("lead_id"),
		r.PostFormValue("status"), r.PostFormValue("color"))
	if errors.Is(err, store.ErrProjectNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		// The project keeps its slug across edits, so resolve it for the bounce-back.
		back := "/" + ws.Slug + "/projects"
		if p, perr := h.board.Project(r.Context(), id); perr == nil {
			back += "?p=" + p.Slug
		}
		redirectProjectErr(w, r, back, err)
		return
	}
	p, err := h.board.Project(r.Context(), id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/projects?p="+p.Slug, http.StatusSeeOther)
}

func (h *handlers) projectArchive(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.ArchiveProject(r.Context(), r.PathValue("id")); err != nil && !errors.Is(err, store.ErrProjectNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+ws.Slug+"/projects", http.StatusSeeOther)
}

func (h *handlers) projectUnarchive(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := h.board.UnarchiveProject(r.Context(), id); err != nil && !errors.Is(err, store.ErrProjectNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	back := "/" + ws.Slug + "/projects"
	if p, perr := h.board.Project(r.Context(), id); perr == nil {
		back += "?p=" + p.Slug
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// itemSetProject files an item under a project (or clears it), from the modal's
// project picker. JSON like the other item-field mutations.
func (h *handlers) itemSetProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetItemProject(r.Context(), r.PathValue("id"), req.ProjectID); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.liveUpsert(r, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// pct is an integer percentage of done out of total (0 when total is 0).
func pct(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 100 / total
}

// redirectProjectErr bounces back to back with a known error code; an unknown
// error is a 500.
func redirectProjectErr(w http.ResponseWriter, r *http.Request, back string, err error) {
	var code string
	switch {
	case errors.Is(err, board.ErrInvalidProjectName):
		code = "invalid_name"
	case errors.Is(err, board.ErrInvalidProjectBrief):
		code = "invalid_brief"
	case errors.Is(err, board.ErrInvalidProjectStatus):
		code = "invalid_status"
	case errors.Is(err, board.ErrInvalidColor):
		code = "invalid_color"
	case errors.Is(err, store.ErrProjectSlugTaken):
		code = "slug_taken"
	case errors.Is(err, store.ErrUserNotFound):
		code = "lead_unknown"
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

func projectError(code string) string {
	switch code {
	case "invalid_name":
		return "Enter a project name (1–80 characters)."
	case "invalid_brief":
		return "That brief is too long."
	case "invalid_status":
		return "Pick a valid project status."
	case "invalid_color":
		return "Pick a colour from the palette."
	case "slug_taken":
		return "A project with that URL already exists — try a slightly different name."
	case "lead_unknown":
		return "That lead is not a known user."
	default:
		return ""
	}
}
