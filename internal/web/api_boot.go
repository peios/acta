package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/store"
)

// apiAgentBoot returns a compact markdown "boot context" for an agent harness to
// inject at session start: who the token authenticates as, the workspaces on the
// instance, and the agent's memory index (names + summaries) across the scopes
// visible without a workspace argument — agent, user, and site.
//
// It is the read-side counterpart to the memory_save tool: a harness curls this
// from a SessionStart hook so the agent boots already knowing what it knows,
// rather than relying on it to remember to call memory_recall. The body is
// text/markdown, formatted for direct injection — no client-side assembly.
// Bodies are omitted (this is an index); the agent memory_get's what it needs.
func (h *handlers) apiAgentBoot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := principalFrom(ctx)

	var b strings.Builder
	b.WriteString("# Your Acta memory\n\n")

	// Identity. An agent's username is "owner/name"; its user-scope memories live
	// under the human owner, so resolve that id for the user-scope listing.
	who := p.Display
	if who == "" {
		who = p.Username
	}
	owner := p.ID
	if humanOwner, _, isAgent := strings.Cut(p.Username, "/"); isAgent {
		fmt.Fprintf(&b, "You are **%s**, an agent acting for **%s**.\n", who, humanOwner)
		if u, err := h.board.User(ctx, p.ID); err == nil && u.AgentOfID != "" {
			owner = u.AgentOfID
		}
	} else {
		fmt.Fprintf(&b, "You are **%s**.\n", who)
	}

	// Workspaces, so the agent knows where work lives without a separate call.
	if list, err := h.workspaces.List(ctx); err == nil && len(list) > 0 {
		parts := make([]string, len(list))
		for i, ws := range list {
			parts[i] = fmt.Sprintf("%s (`%s`)", ws.Name, ws.Slug)
		}
		fmt.Fprintf(&b, "Workspaces: %s.\n", strings.Join(parts, ", "))
	}
	b.WriteString("\n")

	// Memory index across the always-visible scopes.
	scopes := []struct{ label, scope, id string }{
		{"agent (your own)", store.ScopeAgent, p.ID},
		{"user (your owner's)", store.ScopeUser, owner},
		{"site (instance-wide)", store.ScopeSite, ""},
	}
	total := 0
	for _, s := range scopes {
		mems, err := h.memories.List(ctx, s.scope, s.id)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if len(mems) == 0 {
			continue
		}
		total += len(mems)
		fmt.Fprintf(&b, "## %s\n", s.label)
		for _, m := range mems {
			if m.Summary != "" {
				fmt.Fprintf(&b, "- **%s** — %s\n", m.Name, m.Summary)
			} else {
				fmt.Fprintf(&b, "- **%s**\n", m.Name)
			}
		}
		b.WriteString("\n")
	}

	if total == 0 {
		b.WriteString("_No memories yet._ Save what you learn as you go.\n\n")
	}
	b.WriteString("Read any of these in full with `memory_get`. For a workspace's or project's shared memories, call `memory_recall` with its slug. Save durable knowledge with `memory_save` — pick the broadest scope it's true for, not your own agent scope by default.\n")

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
