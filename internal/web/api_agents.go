package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/store"
)

// This file is the JSON-API face of agent and token management — the same
// operations the Settings UI offers (create an agent, mint a personal access
// token), exposed over Bearer auth so the `acta` CLI can provision an MCP
// integration end to end: pick or create an agent, mint its token, write the
// client config. Privileged actions stay human-only by construction — an agent
// can't own agents (agent.Create rejects it) and can only mint tokens for agents
// its caller owns (agents.Get is the ownership guard).

type agentAPI struct {
	ID      string `json:"id"`
	Handle  string `json:"handle"` // owner/name
	Name    string `json:"name"`   // local part
	Display string `json:"display,omitempty"`
	Created string `json:"created_at"`
}

// tokenAPI carries a minted token. Token (the plaintext) is present only in the
// mint response — it is shown once and never stored in the clear.
type tokenAPI struct {
	ID      string `json:"id"`
	Token   string `json:"token,omitempty"`
	Prefix  string `json:"prefix"`
	Name    string `json:"name,omitempty"`
	Created string `json:"created_at"`
}

func (h *handlers) apiListAgents(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	list, err := h.agents.List(r.Context(), p.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]agentAPI, len(list))
	for i, a := range list {
		out[i] = toAgentAPI(a)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Display string `json:"display"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	p := principalFrom(r.Context())
	a, err := h.agents.Create(r.Context(), p.ID, req.Name, req.Display)
	if err != nil {
		apiAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAgentAPI(a))
}

func (h *handlers) apiCreateAgentToken(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	a, err := h.agents.Get(r.Context(), r.PathValue("id"), p.ID)
	if err != nil {
		// ErrNotOwned and a missing user are indistinguishable to the caller:
		// you can only mint tokens for an agent you own.
		apiError(w, http.StatusNotFound, "agent not found")
		return
	}
	name, ok := readTokenName(w, r)
	if !ok {
		return
	}
	plaintext, t, err := h.tokens.Mint(r.Context(), a.ID, name)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toTokenAPI(t, plaintext))
}

func (h *handlers) apiCreateSelfToken(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	name, ok := readTokenName(w, r)
	if !ok {
		return
	}
	plaintext, t, err := h.tokens.Mint(r.Context(), p.ID, name)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toTokenAPI(t, plaintext))
}

// --- helpers ---

func toAgentAPI(a store.User) agentAPI {
	name := a.Username
	if _, local, ok := strings.Cut(a.Username, "/"); ok {
		name = local
	}
	return agentAPI{
		ID:      a.ID,
		Handle:  a.Username,
		Name:    name,
		Display: a.Display,
		Created: a.CreatedAt.Format(time.RFC3339),
	}
}

func toTokenAPI(t store.APIToken, plaintext string) tokenAPI {
	return tokenAPI{
		ID:      t.ID,
		Token:   plaintext,
		Prefix:  t.Prefix,
		Name:    t.Name,
		Created: t.CreatedAt.Format(time.RFC3339),
	}
}

// readTokenName decodes the optional {"name": "..."} body for a mint request. An
// empty body is fine (the token gets a default label); a present body must be
// valid JSON.
func readTokenName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Name string `json:"name"`
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return "", false
	}
	return req.Name, true
}

// apiAgentErr maps an agent-service error to a JSON response.
func apiAgentErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrInvalidName):
		apiError(w, http.StatusBadRequest, "invalid agent name")
	case errors.Is(err, agent.ErrNameTaken):
		apiError(w, http.StatusConflict, "agent name already taken")
	case errors.Is(err, agent.ErrOwnerIsAgent):
		apiError(w, http.StatusForbidden, "an agent cannot own agents")
	default:
		apiError(w, http.StatusInternalServerError, "internal error")
	}
}
