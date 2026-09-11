package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"acta/internal/harnesspipe"
	"acta/internal/hyperharness"
	"acta/internal/threads"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

func (h *Handler) harnessRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/harnesses", h.harnessList)
	m.HandleFunc("GET /api/harnesses/connect", h.harnessConnect)
	m.HandleFunc("GET /api/harnesses/live", h.harnessLive)
}
func harnessStream(path string) bool {
	return path == "/api/harnesses/connect" || path == "/api/harnesses/live"
}

func (h *Handler) harnessOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), hyperharness.ResponseTimeout)
	defer cancel()
	a, e := h.auth.Current(ctx, token(r, h.sessionCookie))
	if e != nil {
		failure(w, e)
		return "", false
	}
	if a.ParentID != nil {
		writeError(w, 403, "human_required", "Harnesses require a human account. Select a human CLI profile.", nil)
		return "", false
	}
	return a.ID, true
}
func (h *Handler) harnessList(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, h.harnesses.Snapshot(owner))
}
func (h *Handler) harnessSocket(w http.ResponseWriter, r *http.Request) *websocket.Conn {
	// Exact origin checks before Accept; native clients have no Origin. Never
	// enable wildcard browser access to cookie-authenticated streams.
	if origin := r.Header.Get("Origin"); origin != "" && !h.config.Origins[origin] {
		writeError(w, 403, "invalid_origin", "This request did not come from this Acta installation.", nil)
		return nil
	}
	if !strings.Contains(","+strings.ReplaceAll(r.Header.Get("Sec-WebSocket-Protocol"), " ", "")+",", ","+hyperharness.Protocol+",") {
		writeError(w, 426, "harness_protocol", "This harness protocol is unsupported. Update the CLI and server together.", nil)
		return nil
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{hyperharness.Protocol}, InsecureSkipVerify: true})
	if e != nil {
		return nil
	}
	c.SetReadLimit(harnesspipe.MaxWire)
	return c
}
func socketWrite(ctx context.Context, c *websocket.Conn, v any) error {
	ctx, cancel := context.WithTimeout(ctx, hyperharness.ResponseTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, v)
}
func (h *Handler) harnessConnect(w http.ResponseWriter, r *http.Request) {
	// Opening a machine connection requires CLI bearer authority, never cookies.
	if r.Header.Get("Origin") != "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer cli_") {
		writeError(w, 401, "cli_required", "Connect with a signed-in CLI profile. Run acta login.", nil)
		return
	}
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	c := h.harnessSocket(w, r)
	if c == nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(r.Context(), hyperharness.ResponseTimeout)
	var hello hyperharness.Hello
	err := wsjson.Read(ctx, c, &hello)
	cancel()
	if err != nil {
		return
	}
	if !hyperharness.ValidHostname(hello.Hostname) {
		_ = c.Close(websocket.StatusPolicyViolation, "Invalid hostname")
		return
	}
	connection, disconnect := h.harnesses.Connect(owner, hello.Hostname)
	defer disconnect()
	if socketWrite(r.Context(), c, map[string]any{"type": "connected", "connection": connection}) != nil {
		return
	}
	h.serveHarnessSocket(r, c, owner, connection.ID, nil)
}
func (h *Handler) harnessLive(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	c := h.harnessSocket(w, r)
	if c == nil {
		return
	}
	defer c.CloseNow()
	changes, unsubscribe := h.harnesses.Subscribe(owner)
	defer unsubscribe()
	if socketWrite(r.Context(), c, h.harnesses.Snapshot(owner)) != nil {
		return
	}
	h.serveHarnessSocket(r, c, owner, "", changes)
}
func (h *Handler) serveHarnessSocket(r *http.Request, c *websocket.Conn, owner, connectionID string, changes <-chan struct{}) {
	ctx, cancel := context.WithCancel(r.Context())
	updates := make(chan hyperharness.ProviderUpdate, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		for {
			var update hyperharness.ProviderUpdate
			if wsjson.Read(ctx, c, &update) != nil {
				return
			}
			select {
			case updates <- update:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { cancel(); c.CloseNow(); <-done }()
	commands := h.harnesses.Commands(connectionID)
	advertised := []threads.Descriptor{}
	replay := func() bool {
		for _, t := range advertised {
			pending, err := h.auth.PendingThreadControls(ctx, owner, t.ID)
			if err != nil {
				return false
			}
			for _, q := range pending {
				if !h.harnesses.Send(owner, connectionID, q) {
					break
				}
			}
		}
		return true
	}
	ticker := time.NewTicker(hyperharness.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case command := <-commands:
			if !h.harnessAuthorized(ctx, r, c, owner) {
				return
			}
			if socketWrite(ctx, c, map[string]any{"type": "control", "control": command}) != nil {
				return
			}
		case update := <-updates:
			if connectionID == "" {
				_ = c.Close(websocket.StatusPolicyViolation, "Unsupported harness message")
				return
			}
			if !h.harnessAuthorized(ctx, r, c, owner) {
				return
			}
			valid := true
			switch update.Type {
			case "providers":
				valid = h.harnesses.UpdateProviders(owner, connectionID, update.Providers)
			case "inventory", "committed":
				if update.Type == "committed" {
					found := false
					for _, t := range update.Threads {
						if t.ID == update.ThreadID && t.Committed {
							found = true
						}
					}
					if !found {
						_ = c.Close(websocket.StatusPolicyViolation, "Invalid commitment announcement")
						return
					}
				}
				valid = len(update.Threads) <= 256
				seen := map[string]bool{}
				for _, t := range update.Threads {
					_, idErr := uuid.Parse(t.ID)
					_, runErr := uuid.Parse(t.RunID)
					if idErr != nil || runErr != nil || seen[t.ID] || !threads.SupportedProvider(t.Provider) || t.ProviderID == "" || len(t.ProviderID) > 512 || t.CreatedAt.IsZero() || !filepath.IsAbs(t.CWD) || !validThreadState(t.State) || len(t.CWD) > 4096 || len(t.Error) > 8192 || t.Revision < 1 || (t.Name != "" && !threads.ValidName(t.Name)) {
						valid = false
					}
					seen[t.ID] = true
				}
				if valid {
					valid = h.harnesses.Claim(owner, connectionID, update.Threads)
				}
				if valid {
					if err := h.auth.DiscoverThreads(ctx, owner, update.Threads); err != nil {
						_ = socketWrite(ctx, c, map[string]string{"type": "error", "code": "unavailable", "message": "Thread discovery could not be persisted."})
						return
					}
					advertised = update.Threads
					if !replay() {
						return
					}

				}
			case "frames":
				valid = h.harnesses.ThreadConnection(owner, update.ThreadID) == connectionID && len(update.Frames) <= 256
				if valid {
					last, err := h.auth.AppendThreadFrames(ctx, owner, update.ThreadID, update.Frames)
					if err != nil {
						_ = socketWrite(ctx, c, map[string]string{"type": "error", "code": "unavailable", "message": "Thread frames could not be persisted."})
						return
					}
					if socketWrite(ctx, c, map[string]any{"type": "frame_ack", "thread_id": update.ThreadID, "sequence": last}) != nil {
						return
					}
				}
			case "result":
				valid = update.Result != nil
				if valid {
					_, a := uuid.Parse(update.Result.ID)
					_, b := uuid.Parse(update.Result.ThreadID)
					valid = a == nil && b == nil && len(update.Result.Error) <= 8192 && (update.Result.Outcome == "" || update.Result.Outcome == "accepted" || update.Result.Outcome == "rejected" || update.Result.Outcome == "uncertain") && (update.Result.Outcome == "" || h.harnesses.ThreadConnection(owner, update.Result.ThreadID) == connectionID)
				}
				if valid {
					if err := h.auth.CompleteThreadControl(ctx, owner, *update.Result); err != nil {
						return
					}
					h.harnesses.RecordResult(owner, connectionID, *update.Result)
				}
			default:
				valid = false
			}
			if !valid {
				_ = c.Close(websocket.StatusPolicyViolation, "Invalid harness advertisement")
				return
			}

		case <-changes:
			// Check authority before delivering even an unsolicited update.
			if !h.harnessAuthorized(ctx, r, c, owner) {
				return
			}
			if socketWrite(ctx, c, h.harnesses.Snapshot(owner)) != nil {
				return
			}
		case <-ticker.C:
			if !replay() {
				return
			}
			if !h.harnessAuthorized(ctx, r, c, owner) {
				return
			}
			pingCtx, cancel := context.WithTimeout(ctx, hyperharness.ResponseTimeout)
			err := c.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
			if socketWrite(ctx, c, map[string]string{"type": "heartbeat"}) != nil {
				return
			}
		}
	}
}
func (h *Handler) harnessAuthorized(ctx context.Context, r *http.Request, c *websocket.Conn, owner string) bool {
	if ctx.Err() != nil {
		return false
	}
	check, cancel := context.WithTimeout(ctx, hyperharness.ResponseTimeout)
	defer cancel()
	a, e := h.auth.Current(check, token(r, h.sessionCookie))
	if e == nil && a.ID == owner && a.ParentID == nil {
		return true
	}
	code, message := "human_required", "Harnesses require a human account."
	status := websocket.StatusPolicyViolation
	if e != nil {
		httpStatus, p := classifyError(e)
		code, message = p.Code, p.Message
		if httpStatus >= 500 {
			status = websocket.StatusInternalError
		}
	}
	_ = socketWrite(ctx, c, map[string]any{"type": "error", "code": code, "message": message})
	_ = c.Close(status, "Connection authorization ended")
	return false
}

func validThreadState(s string) bool {
	switch s {
	case "starting", "running", "killing", "exited", "error", "uncertain":
		return true
	}
	return false
}
