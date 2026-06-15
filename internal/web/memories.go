package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/memory"
	"github.com/peios/acta/internal/store"
)

// --- account: memories ---
//
// One page at /account/memories with a tab strip: "Mine" (the signed-in user's
// own memories, scope "user") and one tab per agent the user owns (scope
// "agent"). ?agent=<id> selects an agent tab. Create/edit/delete all run through
// this page; the target scope is carried by the selected tab (create) or read
// off the memory being edited (edit/delete), and every mutation redirects back
// to the tab it happened on. Server-rendered forms, like the rest of /account.

type memoriesData struct {
	chrome
	Principal *identity.Principal
	Tabs      []memoryTab
	AgentID   string // "" = the Mine tab; otherwise the selected agent's id
	Memories  []memoryView
	Err       string
}

// memoryTab is one entry in the strip. AgentID is "" for the Mine tab.
type memoryTab struct {
	Label  string
	Href   string
	Active bool
}

type memoryEditData struct {
	chrome
	Principal *identity.Principal
	Memory    store.Memory
	ListURL   string // back/cancel target — the tab the memory lives in
	Err       string
}

// memoryView is one memory as the list shows it: its name, one-line summary, and
// edit-time metadata (who last touched it, when) — the body isn't rendered
// inline (the name links to the editor, which holds the full markdown).
type memoryView struct {
	ID      string
	Name    string
	Summary string
	By      string // display name of who last updated it ("" if unknown)
	Rel     string
	Abs     string
	Updated bool
}

func memoryToView(m store.Memory, by string) memoryView {
	return memoryView{
		ID:      m.ID,
		Name:    m.Name,
		Summary: m.Summary,
		By:      by,
		Rel:     relativeWhen(m.UpdatedAt),
		Abs:     formatWhen(m.UpdatedAt),
		Updated: m.UpdatedAt.Sub(m.CreatedAt) > time.Second,
	}
}

// memListURL is a tab's list URL: Mine ("") or one agent's tab.
func memListURL(agentID string) string {
	if agentID == "" {
		return "/account/memories"
	}
	return "/account/memories?agent=" + agentID
}

// memListErr is a tab's list URL flagged with an invalid-input error, choosing
// the right query separator.
func memListErr(agentID string) string {
	if agentID == "" {
		return "/account/memories?err=invalid"
	}
	return "/account/memories?agent=" + agentID + "&err=invalid"
}

// memListURLForMemory is the tab a memory belongs to — where its edit/delete
// return to.
func memListURLForMemory(m store.Memory) string {
	if m.Scope == store.ScopeAgent {
		return memListURL(m.ScopeID)
	}
	return memListURL("")
}

// agentTabLabel is the agent's local handle (the part after "owner/"), short and
// unambiguous within the owner's own tab strip.
func agentTabLabel(a store.User) string {
	if i := strings.LastIndex(a.Username, "/"); i >= 0 {
		return a.Username[i+1:]
	}
	return a.Username
}

// memoryNotFoundOr500 maps ownership/lookup failures to 404 and anything else to
// a 500.
func memoryNotFoundOr500(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agent.ErrNotOwned),
		errors.Is(err, store.ErrUserNotFound),
		errors.Is(err, store.ErrProjectNotFound),
		errors.Is(err, store.ErrMemoryNotFound):
		http.NotFound(w, r)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// resolveOwnedMemory loads a memory and confirms the signed-in user owns it —
// either it's their own user-scoped memory, or it belongs to one of their
// agents. Anything else (another user's, an unowned agent's) reads as not found,
// so a memory id can't be edited or deleted across owners.
func (h *handlers) resolveOwnedMemory(r *http.Request, mid string) (store.Memory, error) {
	m, err := h.memories.Get(r.Context(), mid)
	if err != nil {
		return store.Memory{}, err
	}
	p := principalFrom(r.Context())
	switch m.Scope {
	case store.ScopeUser:
		if m.ScopeID == p.ID {
			return m, nil
		}
	case store.ScopeAgent:
		if _, err := h.agents.Get(r.Context(), m.ScopeID, p.ID); err == nil {
			return m, nil
		}
	}
	return store.Memory{}, store.ErrMemoryNotFound
}

func (h *handlers) accountMemories(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	agents, err := h.agents.List(r.Context(), p.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Resolve the selected tab; an unknown ?agent= quietly falls back to Mine.
	agentID := r.URL.Query().Get("agent")
	if agentID != "" {
		owned := false
		for _, a := range agents {
			if a.ID == agentID {
				owned = true
				break
			}
		}
		if !owned {
			http.Redirect(w, r, memListURL(""), http.StatusSeeOther)
			return
		}
	}
	scope, scopeID := store.ScopeUser, p.ID
	if agentID != "" {
		scope, scopeID = store.ScopeAgent, agentID
	}

	tabs := []memoryTab{{Label: "Mine", Href: memListURL(""), Active: agentID == ""}}
	for _, a := range agents {
		tabs = append(tabs, memoryTab{Label: agentTabLabel(a), Href: memListURL(a.ID), Active: a.ID == agentID})
	}

	mems, err := h.memories.List(r.Context(), scope, scopeID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	views := make([]memoryView, len(mems))
	for i, m := range mems {
		views[i] = memoryToView(m, h.authorName(r.Context(), m.UpdatedBy))
	}

	ch, err := h.chromeFor(r, "account", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "account_memories.html", memoriesData{
		chrome:    ch,
		Principal: p,
		Tabs:      tabs,
		AgentID:   agentID,
		Memories:  views,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) accountMemoryCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	agentID := r.PostFormValue("agent")
	scope, scopeID := store.ScopeUser, p.ID
	if agentID != "" {
		a, err := h.agents.Get(r.Context(), agentID, p.ID)
		if err != nil {
			memoryNotFoundOr500(w, r, err)
			return
		}
		scope, scopeID = store.ScopeAgent, a.ID
	}
	_, err := h.memories.Create(r.Context(), scope, scopeID,
		r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), p.ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, memListErr(agentID), http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, memListURL(agentID), http.StatusSeeOther)
}

func (h *handlers) accountMemoryEdit(w http.ResponseWriter, r *http.Request) {
	m, err := h.resolveOwnedMemory(r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	ch, err := h.chromeFor(r, "account", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "account_memory_edit.html", memoryEditData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Memory:    m,
		ListURL:   memListURLForMemory(m),
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) accountMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	m, err := h.resolveOwnedMemory(r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	_, err = h.memories.Update(r.Context(), m.ID, r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, "/account/memories/"+m.ID+"?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, memListURLForMemory(m), http.StatusSeeOther)
}

func (h *handlers) accountMemoryDelete(w http.ResponseWriter, r *http.Request) {
	m, err := h.resolveOwnedMemory(r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	if err := h.memories.Delete(r.Context(), m.ID); err != nil &&
		!errors.Is(err, store.ErrMemoryNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, memListURLForMemory(m), http.StatusSeeOther)
}

func memoryError(code string) string {
	switch code {
	case "invalid":
		return "A memory needs a name (up to 200 characters) and a body within the size limit."
	default:
		return ""
	}
}

// --- workspace memories ---
//
// Workspace-scoped notes at /{slug}/memories, reached from the sidebar (scope
// "workspace", scope_id the workspace id). Shared by everyone who can see the
// workspace. Same list/create/edit/delete shape as the account memories, but in
// the workspace chrome. The edit page and mutations use 4-segment paths
// (/memories/{mid}/edit, /memories/{mid}/delete) to stay clear of the mux's
// /{slug}/{board} wildcard — see server.go.

type wsMemoriesData struct {
	chrome
	Principal *identity.Principal
	Memories  []memoryView
	Err       string
}

type wsMemoryEditData struct {
	chrome
	Principal *identity.Principal
	Memory    store.Memory
	Err       string
}

func wsMemBase(slug string) string { return "/" + slug + "/memories" }

// resolveWorkspaceMemory loads a memory and confirms it belongs to this
// workspace, so an id from another scope or workspace can't be reached here.
func (h *handlers) resolveWorkspaceMemory(ws store.Workspace, r *http.Request, mid string) (store.Memory, error) {
	m, err := h.memories.Get(r.Context(), mid)
	if err != nil {
		return store.Memory{}, err
	}
	if m.Scope != store.ScopeWorkspace || m.ScopeID != ws.ID {
		return store.Memory{}, store.ErrMemoryNotFound
	}
	return m, nil
}

func (h *handlers) workspaceMemories(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	ch, err := h.chromeFor(r, "memories", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mems, err := h.memories.List(r.Context(), store.ScopeWorkspace, ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	views := make([]memoryView, len(mems))
	for i, m := range mems {
		views[i] = memoryToView(m, h.authorName(r.Context(), m.UpdatedBy))
	}
	render(w, http.StatusOK, "workspace_memories.html", wsMemoriesData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Memories:  views,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) workspaceMemoryCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, err := h.memories.Create(r.Context(), store.ScopeWorkspace, ws.ID,
		r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, wsMemBase(ws.Slug)+"?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, wsMemBase(ws.Slug), http.StatusSeeOther)
}

func (h *handlers) workspaceMemoryEdit(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	m, err := h.resolveWorkspaceMemory(ws, r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	ch, err := h.chromeFor(r, "memories", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "workspace_memory_edit.html", wsMemoryEditData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Memory:    m,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) workspaceMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	m, err := h.resolveWorkspaceMemory(ws, r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	_, err = h.memories.Update(r.Context(), m.ID, r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, wsMemBase(ws.Slug)+"/"+m.ID+"/edit?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, wsMemBase(ws.Slug), http.StatusSeeOther)
}

func (h *handlers) workspaceMemoryDelete(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	m, err := h.resolveWorkspaceMemory(ws, r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	if err := h.memories.Delete(r.Context(), m.ID); err != nil &&
		!errors.Is(err, store.ErrMemoryNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, wsMemBase(ws.Slug), http.StatusSeeOther)
}

// --- project memories ---
//
// Project-scoped notes at /{slug}/projects/{id}/memories, reached from the
// project page (scope "project", scope_id the project id). Paths nest under the
// project — id in the path, like the other project mutations — which keeps them
// clear of the /{slug}/{board} wildcard. chromeFor uses the "projects" section so
// the sidebar's Projects item stays highlighted.

type projMemoriesData struct {
	chrome
	Principal *identity.Principal
	Project   store.Project
	Memories  []memoryView
	Err       string
}

type projMemoryEditData struct {
	chrome
	Principal *identity.Principal
	Project   store.Project
	Memory    store.Memory
	Err       string
}

func projMemBase(slug, projectID string) string {
	return "/" + slug + "/projects/" + projectID + "/memories"
}

// resolveProject loads the project named in the {id} path value and confirms it
// belongs to this workspace.
func (h *handlers) resolveProject(ws store.Workspace, r *http.Request) (store.Project, error) {
	p, err := h.board.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		return store.Project{}, err
	}
	if p.WorkspaceID != ws.ID {
		return store.Project{}, store.ErrProjectNotFound
	}
	return p, nil
}

// resolveProjectMemory loads a memory and confirms it belongs to this project.
func (h *handlers) resolveProjectMemory(proj store.Project, r *http.Request, mid string) (store.Memory, error) {
	m, err := h.memories.Get(r.Context(), mid)
	if err != nil {
		return store.Memory{}, err
	}
	if m.Scope != store.ScopeProject || m.ScopeID != proj.ID {
		return store.Memory{}, store.ErrMemoryNotFound
	}
	return m, nil
}

func (h *handlers) projectMemories(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	proj, err := h.resolveProject(ws, r)
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	ch, err := h.chromeFor(r, "projects", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mems, err := h.memories.List(r.Context(), store.ScopeProject, proj.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	views := make([]memoryView, len(mems))
	for i, m := range mems {
		views[i] = memoryToView(m, h.authorName(r.Context(), m.UpdatedBy))
	}
	render(w, http.StatusOK, "project_memories.html", projMemoriesData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Project:   proj,
		Memories:  views,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) projectMemoryCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	proj, err := h.resolveProject(ws, r)
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, err = h.memories.Create(r.Context(), store.ScopeProject, proj.ID,
		r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, projMemBase(ws.Slug, proj.ID)+"?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, projMemBase(ws.Slug, proj.ID), http.StatusSeeOther)
}

func (h *handlers) projectMemoryEdit(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	proj, err := h.resolveProject(ws, r)
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	m, err := h.resolveProjectMemory(proj, r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	ch, err := h.chromeFor(r, "projects", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "project_memory_edit.html", projMemoryEditData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Project:   proj,
		Memory:    m,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) projectMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	proj, err := h.resolveProject(ws, r)
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	m, err := h.resolveProjectMemory(proj, r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	_, err = h.memories.Update(r.Context(), m.ID, r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, projMemBase(ws.Slug, proj.ID)+"/"+m.ID+"/edit?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, projMemBase(ws.Slug, proj.ID), http.StatusSeeOther)
}

func (h *handlers) projectMemoryDelete(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	proj, err := h.resolveProject(ws, r)
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	m, err := h.resolveProjectMemory(proj, r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	if err := h.memories.Delete(r.Context(), m.ID); err != nil &&
		!errors.Is(err, store.ErrMemoryNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, projMemBase(ws.Slug, proj.ID), http.StatusSeeOther)
}

// --- settings: site memories ---
//
// Instance-wide memories (scope "site", scope_id ""), managed from /settings —
// shared across the whole instance (e.g. the "how to use Acta" guide and the
// memory-scope conventions). Same shape as the account memories, in the settings
// chrome. No permissions yet, so any signed-in user can manage them.

type siteMemoriesData struct {
	chrome
	Principal *identity.Principal
	Memories  []memoryView
	Err       string
}

type siteMemoryEditData struct {
	chrome
	Principal *identity.Principal
	Memory    store.Memory
	Err       string
}

const siteMemBase = "/settings/memories"

// resolveSiteMemory loads a memory and confirms it's site-scoped, so an id from
// another scope can't be reached through the settings routes.
func (h *handlers) resolveSiteMemory(r *http.Request, mid string) (store.Memory, error) {
	m, err := h.memories.Get(r.Context(), mid)
	if err != nil {
		return store.Memory{}, err
	}
	if m.Scope != store.ScopeSite || m.ScopeID != "" {
		return store.Memory{}, store.ErrMemoryNotFound
	}
	return m, nil
}

func (h *handlers) settingsMemories(w http.ResponseWriter, r *http.Request) {
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mems, err := h.memories.List(r.Context(), store.ScopeSite, "")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	views := make([]memoryView, len(mems))
	for i, m := range mems {
		views[i] = memoryToView(m, h.authorName(r.Context(), m.UpdatedBy))
	}
	render(w, http.StatusOK, "settings_memories.html", siteMemoriesData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Memories:  views,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) settingsMemoryCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, err := h.memories.Create(r.Context(), store.ScopeSite, "",
		r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, siteMemBase+"?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, siteMemBase, http.StatusSeeOther)
}

func (h *handlers) settingsMemoryEdit(w http.ResponseWriter, r *http.Request) {
	m, err := h.resolveSiteMemory(r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "settings_memory_edit.html", siteMemoryEditData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Memory:    m,
		Err:       memoryError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) settingsMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	m, err := h.resolveSiteMemory(r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	_, err = h.memories.Update(r.Context(), m.ID, r.PostFormValue("name"), r.PostFormValue("summary"), r.PostFormValue("body"), principalFrom(r.Context()).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			http.Redirect(w, r, siteMemBase+"/"+m.ID+"?err=invalid", http.StatusSeeOther)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, siteMemBase, http.StatusSeeOther)
}

func (h *handlers) settingsMemoryDelete(w http.ResponseWriter, r *http.Request) {
	m, err := h.resolveSiteMemory(r, r.PathValue("mid"))
	if err != nil {
		memoryNotFoundOr500(w, r, err)
		return
	}
	if err := h.memories.Delete(r.Context(), m.ID); err != nil &&
		!errors.Is(err, store.ErrMemoryNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, siteMemBase, http.StatusSeeOther)
}
