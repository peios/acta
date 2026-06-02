package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/peios/acta/internal/account"
	"github.com/peios/acta/internal/authn/local"
	"github.com/peios/acta/internal/store"
)

// --- global settings: principals ---

type principalsData struct {
	chrome
	Users []principalRow
	Err   string
}

// principalRow is a human user, with their agents nested beneath them. Only
// users carry a disable/enable action; agents are shown for context.
type principalRow struct {
	store.User
	IsSelf   bool
	Disabled bool
	Agents   []store.User
}

func (h *handlers) settingsPrincipals(w http.ResponseWriter, r *http.Request) {
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	users, err := h.accounts.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	me := principalFrom(r.Context())

	agentsByOwner := map[string][]store.User{}
	for _, u := range users {
		if u.AgentOfID != "" {
			agentsByOwner[u.AgentOfID] = append(agentsByOwner[u.AgentOfID], u)
		}
	}
	rows := make([]principalRow, 0, len(users))
	for _, u := range users {
		if u.AgentOfID != "" {
			continue // agents are nested under their owner
		}
		rows = append(rows, principalRow{
			User:     u,
			IsSelf:   me != nil && me.ID == u.ID,
			Disabled: u.DisabledAt != nil,
			Agents:   agentsByOwner[u.ID],
		})
	}
	render(w, http.StatusOK, "principals.html", principalsData{
		chrome: ch,
		Users:  rows,
		Err:    principalError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) principalCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, err := h.accounts.Create(r.Context(),
		r.PostFormValue("username"), r.PostFormValue("display"), r.PostFormValue("password"))
	h.afterPrincipalAction(w, r, err)
}

func (h *handlers) principalDisable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if me := principalFrom(r.Context()); me != nil && me.ID == id {
		http.Redirect(w, r, "/settings/principals?err=self", http.StatusSeeOther)
		return
	}
	h.afterPrincipalAction(w, r, h.accounts.Disable(r.Context(), id))
}

func (h *handlers) principalEnable(w http.ResponseWriter, r *http.Request) {
	h.afterPrincipalAction(w, r, h.accounts.Enable(r.Context(), r.PathValue("id")))
}

// afterPrincipalAction redirects back to the page: bare on success, with an
// ?err code on a known failure, or a 500 for anything unexpected.
func (h *handlers) afterPrincipalAction(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		http.Redirect(w, r, "/settings/principals", http.StatusSeeOther)
		return
	}
	code := principalErrCode(err)
	if code == "" {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings/principals?err="+code, http.StatusSeeOther)
}

func principalErrCode(err error) string {
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		return "username_taken"
	case errors.Is(err, account.ErrInvalidUsername):
		return "invalid_username"
	case errors.Is(err, account.ErrWeakPassword):
		return "weak_password"
	case errors.Is(err, account.ErrLastActiveUser):
		return "last_active"
	case errors.Is(err, account.ErrNotHuman):
		return "not_human"
	default:
		return ""
	}
}

func principalError(code string) string {
	switch code {
	case "username_taken":
		return "That username is already taken."
	case "invalid_username":
		return "Username must be lowercase letters, digits, dot, underscore or hyphen — no spaces or slashes."
	case "weak_password":
		return fmt.Sprintf("Password must be at least %d characters.", local.MinPasswordLen)
	case "last_active":
		return "You can't disable the last active user."
	case "not_human":
		return "Agents can't be disabled here."
	case "self":
		return "You can't disable your own account."
	default:
		return ""
	}
}
