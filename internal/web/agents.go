package web

import (
	"errors"
	"net/http"

	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- account: agents ---

type agentsData struct {
	chrome
	Principal *identity.Principal
	Agents    []agentRow
	Err       string
}

// agentRow is one of the owner's agents plus the owner's watch on it (the
// principal subscription): whether they watch it and the category filter, for
// the inline Watch control.
type agentRow struct {
	store.User
	Watching  bool
	WatchCats []catToggle
}

func (h *handlers) accountAgents(w http.ResponseWriter, r *http.Request) {
	ch, err := h.chromeFor(r, "account", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p := principalFrom(r.Context())
	agents, err := h.agents.List(r.Context(), p.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]agentRow, 0, len(agents))
	for _, a := range agents {
		sub, ok, _ := h.board.SubscriptionFor(r.Context(), p.ID, store.SubjectPrincipal, a.ID)
		rows = append(rows, agentRow{User: a, Watching: ok, WatchCats: catToggles(sub.Events)})
	}
	render(w, http.StatusOK, "agents.html", agentsData{
		chrome:    ch,
		Principal: p,
		Agents:    rows,
		Err:       agentError(r.URL.Query().Get("err")),
	})
}

// agentSubscribe drives the Watch control on the agents page — the owner's
// principal subscription to one of their agents.
func (h *handlers) agentSubscribe(w http.ResponseWriter, r *http.Request) {
	h.handleSubscribeJSON(w, r, store.SubjectPrincipal, r.PathValue("id"))
}

func (h *handlers) agentCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	a, err := h.agents.Create(r.Context(), p.ID, r.PostFormValue("name"), r.PostFormValue("display"))
	if err != nil {
		code := agentErrCode(err)
		if code == "" { // unexpected
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/account/agents?err="+code, http.StatusSeeOther)
		return
	}
	// Owners watch their own agents (status changes by default; tune or mute with
	// the Watch control on the agents page). Best-effort — never fails create.
	_, _ = h.board.Subscribe(r.Context(), p.ID, store.SubjectPrincipal, a.ID)
	// Land on the new agent's page so the owner can mint its first token.
	http.Redirect(w, r, "/account/agents/"+a.ID, http.StatusSeeOther)
}

func (h *handlers) agentDelete(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	err := h.agents.Delete(r.Context(), r.PathValue("id"), p.ID)
	if err != nil && !errors.Is(err, agent.ErrNotOwned) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/agents", http.StatusSeeOther)
}

type agentDetailData struct {
	chrome
	Principal    *identity.Principal
	Agent        store.User
	TokenSection tokensView
}

func (h *handlers) agentDetail(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildAgentDetail(r, "")
	switch {
	case errors.Is(err, agent.ErrNotOwned), errors.Is(err, store.ErrUserNotFound):
		http.NotFound(w, r)
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
	default:
		render(w, http.StatusOK, "agent_detail.html", data)
	}
}

// buildAgentDetail assembles an agent's page after confirming the caller owns
// it. newToken, when set, is a freshly minted token's plaintext to reveal once.
func (h *handlers) buildAgentDetail(r *http.Request, newToken string) (agentDetailData, error) {
	p := principalFrom(r.Context())
	a, err := h.agents.Get(r.Context(), r.PathValue("id"), p.ID)
	if err != nil {
		return agentDetailData{}, err
	}
	tokens, err := h.tokens.List(r.Context(), a.ID)
	if err != nil {
		return agentDetailData{}, err
	}
	ch, err := h.chromeFor(r, "account", nil)
	if err != nil {
		return agentDetailData{}, err
	}
	base := "/account/agents/" + a.ID + "/tokens"
	return agentDetailData{
		chrome:    ch,
		Principal: p,
		Agent:     a,
		TokenSection: tokensView{
			CSRFToken:    ch.CSRFToken,
			Tokens:       tokens,
			NewToken:     newToken,
			CreateAction: base,
			DeleteBase:   base,
			Placeholder:  "Token name (e.g. CI deploy)",
		},
	}, nil
}

func (h *handlers) agentTokenCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	a, err := h.agents.Get(r.Context(), r.PathValue("id"), p.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	plaintext, _, err := h.tokens.Mint(r.Context(), a.ID, r.PostFormValue("name"))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Re-render with the one-time plaintext (no redirect — it can't be re-shown).
	data, err := h.buildAgentDetail(r, plaintext)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "agent_detail.html", data)
}

func (h *handlers) agentTokenDelete(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	a, err := h.agents.Get(r.Context(), r.PathValue("id"), p.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.tokens.Revoke(r.Context(), r.PathValue("tokenID"), a.ID)
	if err != nil && !errors.Is(err, store.ErrAPITokenNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/agents/"+a.ID, http.StatusSeeOther)
}

// agentErrCode maps a known agent error to a query code, or "" for anything
// unexpected (which the caller turns into a 500).
func agentErrCode(err error) string {
	switch {
	case errors.Is(err, agent.ErrInvalidName):
		return "invalid_name"
	case errors.Is(err, agent.ErrNameTaken):
		return "name_taken"
	case errors.Is(err, agent.ErrOwnerIsAgent):
		return "owner_is_agent"
	default:
		return ""
	}
}

func agentError(code string) string {
	switch code {
	case "invalid_name":
		return "Name must be lowercase letters, digits and hyphens (e.g. deploy-bot)."
	case "name_taken":
		return "You already have an agent with that name."
	case "owner_is_agent":
		return "Agents can't own agents."
	default:
		return ""
	}
}
