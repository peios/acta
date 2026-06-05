package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// itemAPI is an item as the JSON API presents it: human-meaningful names rather
// than internal ids for status and principals (a CLI/agent thinks in "Doing"
// and "jack/deploy-bot", not uuids). Items themselves stay id-addressed.
type itemAPI struct {
	ID        string    `json:"id"`
	Ref       string    `json:"ref,omitempty"` // human id, e.g. "ACTA-12" (also accepted in the path)
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Assignee  string    `json:"assignee,omitempty"`
	Project   string    `json:"project,omitempty"` // project slug, "" if unfiled (set via the project endpoint)
	Milestone bool      `json:"milestone,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"`
	Subtasks  []itemAPI `json:"subtasks,omitempty"` // populated by the item-show endpoint
	// Direct-subtask progress, populated by the board listing (done = last lane).
	SubtasksDone  int `json:"subtasks_done,omitempty"`
	SubtasksTotal int `json:"subtasks_total,omitempty"`
}

type workspaceAPI struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// projectAPI is a project as the JSON API / MCP surface presents it: addressed
// by slug (like a workspace or board), with its lead by username and top-level
// item progress (done/total). Shared by REST and MCP.
type projectAPI struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Status string `json:"status"`          // lifecycle: planned/active/paused/done
	Lead   string `json:"lead,omitempty"`  // username of the lead, "" if none
	Brief  string `json:"brief,omitempty"` // markdown description
	Done   int    `json:"done"`            // top-level items in a "done" lane
	Total  int    `json:"total"`           // top-level items in the project
}

func toProjectAPI(p store.Project, userName map[string]string, c store.SubtaskCount) projectAPI {
	return projectAPI{
		Slug: p.Slug, Name: p.Name, Status: p.Status,
		Lead: userName[p.LeadID], Brief: p.Brief,
		Done: c.Done, Total: c.Total,
	}
}

// projectAPIFor renders a single project, resolving its lead id to a username
// (the list path uses toProjectAPI with a prebuilt map instead). For the
// create responses, where only one project is in hand.
func (h *handlers) projectAPIFor(ctx context.Context, p store.Project, c store.SubtaskCount) projectAPI {
	lead := ""
	if p.LeadID != "" {
		if u, err := h.board.User(ctx, p.LeadID); err == nil {
			lead = u.Username
		}
	}
	return projectAPI{
		Slug: p.Slug, Name: p.Name, Status: p.Status,
		Lead: lead, Brief: p.Brief, Done: c.Done, Total: c.Total,
	}
}

func (h *handlers) apiWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := h.workspaces.List(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]workspaceAPI, len(list))
	for i, ws := range list {
		out[i] = workspaceAPI{Slug: ws.Slug, Name: ws.Name}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiListItems(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	items, err := h.board.Items(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statusName := make(map[string]string, len(statuses))
	for _, s := range statuses {
		statusName[s.ID] = s.Name
	}
	userName := make(map[string]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	projectSlug, err := h.projectSlugs(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// An optional ?project=<slug> narrows to one project's items.
	projectFilter := ""
	if s := strings.TrimSpace(r.URL.Query().Get("project")); s != "" {
		projectFilter, err = h.projectIDBySlug(ctx, ws.ID, s)
		if errors.Is(err, errUnknownProject) {
			apiError(w, http.StatusBadRequest, "unknown project: "+s)
			return
		}
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	// "Done" is the last lane, matching the board's progress badge.
	doneStatusID := ""
	if len(statuses) > 0 {
		doneStatusID = statuses[len(statuses)-1].ID
	}
	counts, err := h.board.SubtaskCounts(ctx, ws.ID, doneStatusID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := []itemAPI{}
	for _, it := range items {
		if projectFilter != "" && it.ProjectID != projectFilter {
			continue
		}
		v := toItemAPI(it, statusName, userName, projectSlug, ws.ItemPrefix)
		if c, ok := counts[it.ID]; ok && c.Total > 0 {
			v.SubtasksDone, v.SubtasksTotal = c.Done, c.Total
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiCreateItem(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Title   string `json:"title"`
		Status  string `json:"status"`
		Project string `json:"project"` // optional project slug to file it under
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	statusID, ok := h.resolveStatus(w, r.Context(), ws, req.Status) // "" -> first lane
	if !ok {
		return
	}
	// Resolve any project before creating, so a bad slug fails cleanly.
	projectID := ""
	if s := strings.TrimSpace(req.Project); s != "" {
		var err error
		if projectID, err = h.projectIDBySlug(r.Context(), ws.ID, s); err != nil {
			apiProjectErr(w, err)
			return
		}
	}
	p := principalFrom(r.Context())
	it, err := h.board.CreateRootItemAs(r.Context(), ws.ID, statusID, req.Title, p.ID)
	if err != nil {
		apiBoardErr(w, err)
		return
	}
	if projectID != "" {
		if err := h.board.SetItemProject(r.Context(), it.ID, projectID); err != nil {
			apiBoardErr(w, err)
			return
		}
		it.ProjectID = projectID
	}
	h.writeItem(w, r.Context(), ws, it, http.StatusCreated)
}

func (h *handlers) apiItem(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	item, err := h.resolveItem(r.Context(), ws, r.PathValue("id"))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && item.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	children, err := h.board.Children(r.Context(), item.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statusName, userName, projectSlug, err := h.nameMaps(r.Context(), ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	view := toItemAPI(item, statusName, userName, projectSlug, ws.ItemPrefix)
	for _, c := range children {
		view.Subtasks = append(view.Subtasks, toItemAPI(c, statusName, userName, projectSlug, ws.ItemPrefix))
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) apiCreateSubtask(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	parent, err := h.resolveItem(r.Context(), ws, r.PathValue("id"))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && parent.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	p := principalFrom(r.Context())
	it, err := h.board.CreateSubtaskAs(r.Context(), parent.ID, req.Title, p.ID)
	if err != nil {
		apiBoardErr(w, err)
		return
	}
	h.writeItem(w, r.Context(), ws, it, http.StatusCreated)
}

func (h *handlers) apiTransition(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		apiError(w, http.StatusBadRequest, "status required")
		return
	}
	item, err := h.resolveItem(r.Context(), ws, r.PathValue("id"))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && item.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statusID, ok := h.resolveStatus(w, r.Context(), ws, req.Status)
	if !ok {
		return
	}
	if err := h.board.SetStatus(r.Context(), item.ID, statusID); err != nil {
		apiBoardErr(w, err)
		return
	}
	updated, err := h.board.Item(r.Context(), item.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.writeItem(w, r.Context(), ws, updated, http.StatusOK)
}

// --- helpers ---

// apiWorkspace resolves the {slug} path value, writing a JSON 404 if missing.
func (h *handlers) apiWorkspace(w http.ResponseWriter, r *http.Request) (store.Workspace, bool) {
	ws, err := h.workspaces.BySlug(r.Context(), strings.ToLower(r.PathValue("slug")))
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		apiError(w, http.StatusNotFound, "workspace not found")
		return store.Workspace{}, false
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return store.Workspace{}, false
	}
	return ws, true
}

// errUnknownStatus / errUnknownUser are sentinels so callers can distinguish a
// bad name (client error) from an internal failure. The wrapped message is
// already human-readable, so the MCP surface returns it verbatim.
var (
	errUnknownStatus      = errors.New("unknown status")
	errUnknownUser        = errors.New("unknown user")
	errUnknownProject     = errors.New("unknown project")
	errUnknownSubjectType = errors.New("unknown subject type (use item, project, or principal)")
	errProjectNeedsWS     = errors.New("a workspace is required to address a project by slug")
)

// subscriptionAPI is a subscription as rendered to API and MCP clients: the
// subject addressed by its natural key (item id, project slug, principal
// username), a human label, and the category filter.
type subscriptionAPI struct {
	Type   string   `json:"type"`            // item | project | principal
	Ref    string   `json:"ref"`             // natural key: item id, project slug, username
	Label  string   `json:"label,omitempty"` // human title/name of the subject
	Events []string `json:"events"`          // categories: comments, status, assignments, items_added, other
}

// resolveSubjectRef turns an API subject reference (type + natural key) into the
// stored subject id, addressing each the documented way: items by id (a human
// ref is accepted too), projects by slug within a workspace, principals by
// username ("me" = the caller).
func (h *handlers) resolveSubjectRef(ctx context.Context, subjectType, ref, workspace string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("ref is required")
	}
	switch subjectType {
	case store.SubjectItem:
		it, err := h.mcpItem(ctx, ref, "")
		if err != nil {
			return "", err
		}
		return it.ID, nil
	case store.SubjectProject:
		if strings.TrimSpace(workspace) == "" {
			return "", errProjectNeedsWS
		}
		ws, err := h.mcpWorkspace(ctx, workspace)
		if err != nil {
			return "", err
		}
		return h.projectIDBySlug(ctx, ws.ID, ref)
	case store.SubjectPrincipal:
		if strings.EqualFold(ref, "me") {
			if p := principalFrom(ctx); p != nil {
				return p.ID, nil
			}
		}
		return h.userIDByName(ctx, ref)
	default:
		return "", errUnknownSubjectType
	}
}

// toSubscriptionAPI renders a stored subscription for API/MCP, resolving the
// subject to its natural-key ref and a label. A subject that no longer resolves
// keeps its stored id as the ref and an empty label (the row is inert but can
// still be removed).
func (h *handlers) toSubscriptionAPI(ctx context.Context, sub store.Subscription) subscriptionAPI {
	out := subscriptionAPI{Type: sub.SubjectType, Ref: sub.SubjectID, Events: sub.Events}
	if out.Events == nil {
		out.Events = []string{}
	}
	switch sub.SubjectType {
	case store.SubjectItem:
		if it, err := h.board.Item(ctx, sub.SubjectID); err == nil {
			out.Label = it.Title
		}
	case store.SubjectProject:
		if pr, err := h.board.Project(ctx, sub.SubjectID); err == nil {
			out.Ref, out.Label = pr.Slug, pr.Name
		}
	case store.SubjectPrincipal:
		if u, err := h.board.User(ctx, sub.SubjectID); err == nil {
			out.Ref, out.Label = u.Username, displayName(u)
		}
	}
	return out
}

// projectIDBySlug resolves a project slug to its id within a workspace,
// case-insensitively. Unknown slugs wrap errUnknownProject.
func (h *handlers) projectIDBySlug(ctx context.Context, workspaceID, slug string) (string, error) {
	p, err := h.board.ProjectBySlug(ctx, workspaceID, strings.ToLower(strings.TrimSpace(slug)))
	if errors.Is(err, store.ErrProjectNotFound) {
		return "", fmt.Errorf("%w: %s", errUnknownProject, slug)
	}
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

// listProjectsAPI builds a workspace's active projects with their lead and
// progress, shared by the REST endpoint and the MCP list_projects tool.
func (h *handlers) listProjectsAPI(ctx context.Context, workspaceID string) ([]projectAPI, error) {
	projects, err := h.board.Projects(ctx, workspaceID, false)
	if err != nil {
		return nil, err
	}
	progress, err := h.board.ProjectProgress(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		return nil, err
	}
	userName := make(map[string]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	out := make([]projectAPI, len(projects))
	for i, p := range projects {
		out[i] = toProjectAPI(p, userName, progress[p.ID])
	}
	return out, nil
}

// createProjectShared resolves the lead (a username, or "me") and creates the
// project, attributed to the caller. Colour is left to auto — it's a UI-only
// nicety the API/MCP/CLI don't set. Shared by REST and MCP.
func (h *handlers) createProjectShared(ctx context.Context, ws store.Workspace, name, brief, statusStr, lead string) (store.Project, error) {
	leadID := ""
	if n := strings.TrimSpace(lead); n != "" {
		if strings.EqualFold(n, "me") {
			leadID = principalFrom(ctx).ID
		} else {
			id, err := h.userIDByName(ctx, n)
			if err != nil {
				return store.Project{}, err
			}
			leadID = id
		}
	}
	createdBy := ""
	if p := principalFrom(ctx); p != nil {
		createdBy = p.ID
	}
	return h.board.CreateProject(ctx, ws.ID, name, brief, leadID, strings.TrimSpace(statusStr), "", createdBy)
}

// apiProjectErr maps a project create/update error to a JSON response.
func apiProjectErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, board.ErrInvalidProjectName):
		apiError(w, http.StatusBadRequest, "invalid project name (1–80 characters)")
	case errors.Is(err, board.ErrInvalidProjectStatus):
		apiError(w, http.StatusBadRequest, "invalid status (use planned/active/paused/done)")
	case errors.Is(err, board.ErrInvalidProjectBrief):
		apiError(w, http.StatusBadRequest, "brief too long")
	case errors.Is(err, board.ErrProjectMismatch):
		apiError(w, http.StatusBadRequest, "project belongs to another workspace")
	case errors.Is(err, store.ErrProjectNotFound):
		apiError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, errUnknownProject), errors.Is(err, errUnknownUser):
		apiError(w, http.StatusBadRequest, err.Error())
	default:
		apiError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *handlers) apiListProjects(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	out, err := h.listProjectsAPI(r.Context(), ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiCreateProject(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Brief  string `json:"brief"`
		Status string `json:"status"`
		Lead   string `json:"lead"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	p, err := h.createProjectShared(r.Context(), ws, req.Name, req.Brief, req.Status, req.Lead)
	if err != nil {
		apiProjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, h.projectAPIFor(r.Context(), p, store.SubtaskCount{}))
}

// apiSetItemProject files an item under a project (by slug) or, with an empty
// project, removes it from its project.
func (h *handlers) apiSetItemProject(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Project string `json:"project"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	item, err := h.resolveItem(r.Context(), ws, r.PathValue("id"))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && item.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	projectID := ""
	if s := strings.TrimSpace(req.Project); s != "" {
		if projectID, err = h.projectIDBySlug(r.Context(), ws.ID, s); err != nil {
			apiProjectErr(w, err)
			return
		}
	}
	if err := h.board.SetItemProject(r.Context(), item.ID, projectID); err != nil {
		apiProjectErr(w, err)
		return
	}
	updated, err := h.board.Item(r.Context(), item.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.writeItem(w, r.Context(), ws, updated, http.StatusOK)
}

// statusIDByName resolves a status name to its id within a workspace,
// case-insensitively. Unknown names wrap errUnknownStatus. This is the shared
// core behind both the REST resolveStatus and the MCP tools.
func (h *handlers) statusIDByName(ctx context.Context, workspaceID, name string) (string, error) {
	statuses, err := h.board.Statuses(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return statusIDInList(statuses, name)
}

// statusIDInList resolves a status name within a specific set of lanes (e.g. one
// board's), case-insensitively. Unknown names wrap errUnknownStatus.
func statusIDInList(statuses []store.Status, name string) (string, error) {
	for _, s := range statuses {
		if strings.EqualFold(s.Name, name) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %s", errUnknownStatus, name)
}

// userIDByName resolves a username to its id, case-insensitively. Unknown names
// wrap errUnknownUser.
func (h *handlers) userIDByName(ctx context.Context, name string) (string, error) {
	users, err := h.board.Users(ctx)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, name) {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %s", errUnknownUser, name)
}

// resolveStatus maps a status name to its id within ws. An empty name yields
// ("", true) — the caller decides what that means (create defaults to the first
// lane). An unknown name writes a 400 and returns ok=false.
func (h *handlers) resolveStatus(w http.ResponseWriter, ctx context.Context, ws store.Workspace, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", true
	}
	id, err := h.statusIDByName(ctx, ws.ID, name)
	switch {
	case err == nil:
		return id, true
	case errors.Is(err, errUnknownStatus):
		apiError(w, http.StatusBadRequest, "unknown status: "+name)
	default:
		apiError(w, http.StatusInternalServerError, "internal error")
	}
	return "", false
}

// nameMaps returns the id→display lookups for rendering an item: status name,
// username, and project slug (the latter includes archived projects so an item
// filed under one still shows it). Shared by REST and MCP item rendering.
func (h *handlers) nameMaps(ctx context.Context, workspaceID string) (statusName, userName, projectSlug map[string]string, err error) {
	statuses, err := h.board.Statuses(ctx, workspaceID)
	if err != nil {
		return nil, nil, nil, err
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	projectSlug, err = h.projectSlugs(ctx, workspaceID)
	if err != nil {
		return nil, nil, nil, err
	}
	statusName = make(map[string]string, len(statuses))
	for _, s := range statuses {
		statusName[s.ID] = s.Name
	}
	userName = make(map[string]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	return statusName, userName, projectSlug, nil
}

// projectSlugs maps a workspace's project ids to their slugs, including archived
// ones, so an item filed under an archived project still renders its project.
func (h *handlers) projectSlugs(ctx context.Context, workspaceID string) (map[string]string, error) {
	projects, err := h.board.Projects(ctx, workspaceID, true)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(projects))
	for _, p := range projects {
		m[p.ID] = p.Slug
	}
	return m, nil
}

func (h *handlers) writeItem(w http.ResponseWriter, ctx context.Context, ws store.Workspace, it store.Item, status int) {
	statusName, userName, projectSlug, err := h.nameMaps(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, status, toItemAPI(it, statusName, userName, projectSlug, ws.ItemPrefix))
}

func toItemAPI(it store.Item, statusName, userName, projectSlug map[string]string, prefix string) itemAPI {
	return itemAPI{
		ID:        it.ID,
		Ref:       refID(prefix, it.RefNum),
		Title:     it.Title,
		Status:    statusName[it.StatusID],
		Assignee:  userName[it.AssigneeID],
		Project:   projectSlug[it.ProjectID],
		Milestone: it.IsMilestone,
		CreatedBy: userName[it.CreatedBy],
		ParentID:  it.ParentID,
	}
}

func readAPIJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// apiBoardErr maps a board/store error to a JSON error response.
func apiBoardErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, board.ErrInvalidTitle):
		apiError(w, http.StatusBadRequest, "invalid title")
	case errors.Is(err, board.ErrNoStatus):
		apiError(w, http.StatusBadRequest, "workspace has no statuses")
	case errors.Is(err, board.ErrStatusMismatch), errors.Is(err, store.ErrStatusNotFound):
		apiError(w, http.StatusBadRequest, "invalid status")
	case errors.Is(err, store.ErrItemNotFound):
		apiError(w, http.StatusNotFound, "item not found")
	default:
		apiError(w, http.StatusInternalServerError, "internal error")
	}
}
