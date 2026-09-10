package httpapi

import (
	"acta2/internal/tasks"
	"net/http"
	"strconv"
	"time"
)

func (h *Handler) taskRoutes(m *http.ServeMux) {
	h.taskFollowRoutes(m)
	m.HandleFunc("POST /api/tasks/{task}/archive", func(w http.ResponseWriter, r *http.Request) {
		var in tasks.Archive
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.ArchiveTask(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/tasks/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		v, e := h.management.SearchTasks(r.Context(), token(r, h.sessionCookie), tasks.SearchQuery{IncludeArchived: q.Get("include_archived") == "true", Query: q.Get("q"), Workspace: q.Get("workspace"), Cursor: q.Get("cursor")})
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	h.taskViewRoutes(m)
	h.taskActivityRoutes(m)
	h.documentRoutes(m)
	m.HandleFunc("GET /api/workspaces/{workspace}/task-config", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.TaskConfig(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/task-prefix", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Prefix  string `json:"prefix"`
			Version int64  `json:"version"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := h.management.TaskPrefix(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), in.Prefix, in.Version); e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/task-statuses", func(w http.ResponseWriter, r *http.Request) {
		var in tasks.StatusChange
		if !decode(w, r, &in) {
			return
		}
		if e := h.management.TaskStatuses(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), in); e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	m.HandleFunc("GET /api/workspaces/{workspace}/tasks", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		f := tasks.Filter{Board: q.Get("board"), Archived: q.Get("archived") == "true", Group: q.Get("group"), GroupID: q.Get("group_id"), Sort: q.Get("sort"), Direction: q.Get("direction"), Cursor: q.Get("cursor"), Parent: q.Get("parent"), State: q.Get("state"), Query: q.Get("q"), Before: before, Priorities: q["priority"], Types: q["type"], Sizes: q["size"], Statuses: q["status"], Assignees: q["assignee"], Unassigned: q.Get("unassigned") == "true"}
		if q.Get("summary") == "true" {
			v, e := h.management.TaskSummaries(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), f)
			if e != nil {
				failure(w, e)
				return
			}
			writeJSON(w, 200, v)
			return
		}
		v, e := h.management.Tasks(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), f)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/tasks", func(w http.ResponseWriter, r *http.Request) {
		var in tasks.Create
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.CreateTask(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 201, v)
	})
	m.HandleFunc("GET /api/tasks/{task}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include") == "subtasks" {
			v, e := h.management.InspectTask(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), r.URL.Query().Get("subtask_cursor"))
			if e != nil {
				failure(w, e)
				return
			}
			writeJSON(w, 200, v)
			return
		}
		v, e := h.management.Task(r.Context(), token(r, h.sessionCookie), r.PathValue("task"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/tasks/{task}", func(w http.ResponseWriter, r *http.Request) {
		var in tasks.Patch
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.PatchTask(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/workspaces/{workspace}/task-groups", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.TaskGroups(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.URL.Query().Get("group"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"groups": v})
	})
	m.HandleFunc("GET /api/workspaces/{workspace}/task-people", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.TaskPeople(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.URL.Query().Get("q"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"people": v})
	})
	// Bounded long polling works across server processes and needs no sticky sessions.
	// Each poll rechecks access. No task content is sent before authorization.
	m.HandleFunc("GET /api/workspaces/{workspace}/task-changes", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		until := time.NewTimer(10 * time.Second)
		defer until.Stop()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			v, e := h.management.TaskConfig(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"))
			if e != nil {
				failure(w, e)
				return
			}
			if v.Revision != after {
				writeJSON(w, 200, v)
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-until.C:
				writeJSON(w, 200, v)
				return
			case <-tick.C:
			}
		}
	})
}
