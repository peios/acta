package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"acta2/internal/conversation"
	"acta2/internal/threads"
	"github.com/google/uuid"
)

func (h *Handler) threadRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/threads/{id}/control/{command}", h.threadControlStatus)
	m.HandleFunc("GET /api/threads", h.threadList)
	m.HandleFunc("POST /api/threads/{id}/delete", h.threadDelete)
	m.HandleFunc("POST /api/threads/create", h.threadCreate)
	m.HandleFunc("POST /api/threads/{id}/control", h.threadControl)
	m.HandleFunc("GET /api/threads/{id}/frames", h.threadFrames)
	m.HandleFunc("GET /api/threads/{id}/conversation", h.threadConversation)
}
func (h *Handler) threadList(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	list, err := h.auth.ListThreads(r.Context(), owner)
	if err != nil {
		failure(w, err)
		return
	}
	type entry struct {
		threads.Descriptor
		ConnectionID string `json:"connection_id,omitempty"`
	}
	out := []entry{}
	for _, t := range list {
		out = append(out, entry{t, h.harnesses.ThreadConnection(owner, t.ID)})
	}
	writeJSON(w, 200, map[string]any{"threads": out})
}
func (h *Handler) threadCreate(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	var q struct {
		ID           string `json:"id"`
		ConnectionID string `json:"connection_id"`
		Provider     string `json:"provider"`
		CWD          string `json:"cwd"`
	}
	if !decode(w, r, &q) {
		return
	}
	if _, err := uuid.Parse(q.ID); err != nil || (!threads.SupportedProvider(q.Provider)) || !filepath.IsAbs(q.CWD) || len(q.CWD) > 4096 {
		writeError(w, 400, "invalid_thread", "Choose a supported provider and an absolute working directory.", nil)
		return
	}
	// Creation is a best-effort live control message. No server thread, timeout
	// tombstone, or pending creation record is written here.
	if !h.harnesses.Send(owner, q.ConnectionID, threads.Control{ID: q.ID, ThreadID: q.ID, Action: "start", Provider: q.Provider, CWD: q.CWD}) {
		writeError(w, 409, "harness_unavailable", "The selected harness is unavailable or busy.", nil)
		return
	}
	writeJSON(w, 202, map[string]string{"id": q.ID})
}
func (h *Handler) threadControl(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, 404, "not_found", "Thread not found.", nil)
		return
	}
	var q struct {
		Name           string                `json:"name"`
		ID             string                `json:"id"`
		LaneID         string                `json:"lane_id"`
		Action         string                `json:"action"`
		RunID          string                `json:"run_id"`
		Images         []threads.InputImage  `json:"images"`
		Text           string                `json:"text"`
		Settings       threads.ModelSettings `json:"settings"`
		PermissionMode string                `json:"permission_mode"`
		ApprovalID     string                `json:"approval_id"`
		QuestionID     string                `json:"question_id"`
		Answers        map[string][]string   `json:"answers"`
		Decision       string                `json:"decision"`
	}
	if !decode(w, r, &q) {
		return
	}
	if _, err := uuid.Parse(q.ID); err != nil || (q.Action != "kill" && q.Action != "resume" && q.Action != "send" && q.Action != "models" && q.Action != "configure" && q.Action != "permissions" && q.Action != "approval" && q.Action != "answer" && q.Action != "interrupt" && q.Action != "rename") {
		writeError(w, 400, "invalid_control", "Invalid thread command.", nil)
		return
	}
	if (q.Action == "rename" && !threads.ValidName(q.Name)) || (q.Action != "rename" && q.Name != "") {
		writeError(w, 400, "invalid_name", "Enter a thread name of 1–120 characters without line breaks.", nil)
		return
	}
	if q.Action != "send" && len(q.Images) != 0 {
		writeError(w, 400, "invalid_control", "Images require a send command.", nil)
		return
	}
	if len(q.LaneID) > 256 || (q.LaneID != "" && q.Action != "send" && q.Action != "interrupt") {
		writeError(w, 400, "invalid_control", "Invalid lane command.", nil)
		return
	}
	if (q.Action != "answer" && (q.QuestionID != "" || q.Answers != nil)) || (q.Action != "permissions" && q.PermissionMode != "") || (q.Action != "approval" && (q.ApprovalID != "" || q.Decision != "")) {
		writeError(w, 400, "invalid_control", "Unexpected permission fields.", nil)
		return
	}
	if q.Action == "permissions" || q.Action == "approval" || q.Action == "answer" {
		_, err := uuid.Parse(q.RunID)
		if (q.Action == "answer" && (q.ID != q.QuestionID || !threads.ValidAnswers(q.Answers))) || err != nil || q.Text != "" || q.Settings != (threads.ModelSettings{}) || (q.Action == "permissions" && !threads.ValidPermissionMode(q.PermissionMode)) || (q.Action == "approval" && (q.ID != q.ApprovalID || (q.Decision != "approve" && q.Decision != "deny"))) {
			writeError(w, 400, "invalid_control", "Invalid permission command for the current run.", nil)
			return
		}
	} else if q.Action == "configure" || q.Action == "models" {
		if _, err := uuid.Parse(q.RunID); err != nil || q.Text != "" || (q.Action == "models" && q.Settings != (threads.ModelSettings{})) || (q.Action == "configure" && (q.Settings.Model == "" || len(q.Settings.Model) > 256 || len(q.Settings.Effort) > 32)) {
			writeError(w, 400, "invalid_settings", "Choose valid model settings for the current run.", nil)
			return
		}
	} else if q.Settings != (threads.ModelSettings{}) {
		writeError(w, 400, "invalid_control", "Unexpected model settings.", nil)
		return
	} else if q.Action == "send" {
		if _, err := uuid.Parse(q.RunID); err != nil || threads.ValidateMessage(q.Text, q.Images) != nil {
			writeError(w, 400, "invalid_message", "Enter text (up to 64 KiB) or up to four PNG, JPEG, GIF or WebP images totalling 4 MiB for the current run.", nil)
			return
		}
	} else if q.Action == "interrupt" {
		if _, err := uuid.Parse(q.RunID); err != nil || q.Text != "" {
			writeError(w, 400, "invalid_control", "Interrupt requires the current provider run.", nil)
			return
		}
	} else if q.RunID != "" || q.Text != "" {
		writeError(w, 400, "invalid_control", "Unexpected message fields.", nil)
		return
	}
	connection := h.harnesses.ThreadConnection(owner, id)

	if connection == "" {
		writeError(w, 409, "harness_unavailable", "This thread's harness is unavailable.", nil)
		return
	}
	control := threads.Control{Name: q.Name, LaneID: q.LaneID, ID: q.ID, ThreadID: id, Action: q.Action, RunID: q.RunID, Text: q.Text, Images: q.Images, Settings: q.Settings, PermissionMode: q.PermissionMode, ApprovalID: q.ApprovalID, Decision: q.Decision, QuestionID: q.QuestionID, Answers: q.Answers}
	if err := h.auth.RequestThreadControl(r.Context(), owner, control); err != nil {
		failure(w, err)
		return
	}
	_ = h.harnesses.Send(owner, connection, control)

	writeJSON(w, 202, map[string]string{"id": q.ID})
}
func (h *Handler) threadFrames(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, 404, "not_found", "Thread not found.", nil)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if after < 0 {
		after = 0
	}
	if _, err := h.auth.Thread(r.Context(), owner, id); err != nil {
		writeError(w, 404, "not_found", "Thread not found.", nil)
		return
	}
	frames, err := h.auth.ThreadFrames(r.Context(), owner, id, after)
	if err != nil {
		failure(w, err)
		return
	}
	type browserFrame struct {
		threads.Frame
		RawJSON string `json:"raw_json"`
	}
	out := make([]browserFrame, 0, len(frames))
	for _, f := range frames {
		var formatted bytes.Buffer
		display := f
		if strings.HasPrefix(f.Kind, "debug/") {
			var data map[string]json.RawMessage
			if err := json.Unmarshal(f.Data, &data); err != nil {
				failure(w, err)
				return
			}
			var providerText string
			if json.Unmarshal(data["raw"], &providerText) == nil && json.Valid([]byte(providerText)) {
				// Only the display copy embeds JSON. The stored/wire raw field
				// remains exact text. RawMessage avoids rounding provider numbers.
				data["raw"] = json.RawMessage(providerText)
				display.Data, err = json.Marshal(data)
				if err != nil {
					failure(w, err)
					return
				}
			}
		}
		raw, err := json.Marshal(display)
		if err != nil {
			failure(w, err)
			return
		}
		if err := json.Indent(&formatted, raw, "", "  "); err != nil {
			failure(w, err)
			return
		}
		out = append(out, browserFrame{Frame: f, RawJSON: formatted.String()})
	}
	writeJSON(w, 200, map[string]any{"frames": out})
}

func (h *Handler) threadControlStatus(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	id, command := r.PathValue("id"), r.PathValue("command")
	_, a := uuid.Parse(id)
	_, b := uuid.Parse(command)
	if a != nil || b != nil {
		writeError(w, 404, "not_found", "Command not found.", nil)
		return
	}
	result, err := h.auth.ThreadControl(r.Context(), owner, id, command)
	if err != nil {
		writeError(w, 404, "not_found", "Command not found.", nil)
		return
	}
	result.Images = nil // The receipt needs identity/outcome, not another copy of image bytes.
	writeJSON(w, 200, result)
}

func (h *Handler) threadConversation(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, e := uuid.Parse(id); e != nil {
		writeError(w, 404, "not_found", "Thread not found.", nil)
		return
	}
	if _, e := h.auth.Thread(r.Context(), owner, id); e != nil {
		writeError(w, 404, "not_found", "Thread not found.", nil)
		return
	}
	q := conversation.Query{LaneID: r.URL.Query().Get("lane"), Before: r.URL.Query().Get("before"), Debug: r.URL.Query().Get("debug") == "true"}
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 0 || q.Before != "" {
			writeError(w, 400, "invalid", "Invalid conversation cursor.", nil)
			return
		}
		q.After = &n
	}
	page, e := h.auth.Conversation(r.Context(), owner, id, q)
	if errors.Is(e, conversation.ErrCursor) {
		writeError(w, 400, "invalid", "Invalid conversation cursor.", nil)
		return
	}
	if e != nil {
		failure(w, e)
		return
	}
	for _, i := range page.Items {
		// Debug display is a copy; the stored raw text is immutable and lossless.
		frame, ok := i.Payload["frame"].(map[string]any)
		if !ok {
			continue
		}
		raw, e := json.Marshal(frame)
		if e != nil {
			failure(w, e)
			return
		}
		var f threads.Frame
		if e = json.Unmarshal(raw, &f); e != nil {
			failure(w, e)
			return
		}
		if strings.HasPrefix(f.Kind, "debug/") {
			var d map[string]json.RawMessage
			_ = json.Unmarshal(f.Data, &d)
			var text string
			if json.Unmarshal(d["raw"], &text) == nil && json.Valid([]byte(text)) {
				d["raw"] = json.RawMessage(text)
				f.Data, _ = json.Marshal(d)
			}
		}
		raw, e = json.MarshalIndent(f, "", "  ")
		if e != nil {
			failure(w, e)
			return
		}
		frame["raw_json"] = string(raw)
	}
	writeJSON(w, 200, page)
}

func (h *Handler) threadDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, 404, "not_found", "Thread not found.", nil)
		return
	}
	if err := h.auth.DeleteThread(r.Context(), owner, id); err != nil {
		if errors.Is(err, threads.ErrNotFound) {
			writeError(w, 404, "not_found", "Thread not found.", nil)
		} else {
			failure(w, err)
		}
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
